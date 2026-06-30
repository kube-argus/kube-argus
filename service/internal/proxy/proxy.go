// Package proxy is a kube-apiserver auth proxy. It verifies a broker-issued
// id_token on each request, swaps it for the request user's ServiceAccount token
// (minted via TokenRequest, cached), and reverse-proxies to the real apiserver.
//
// This lets an OIDC client (e.g. Headlamp) authenticate via the broker while the
// kube-apiserver only ever sees natively-accepted SA tokens — so it works on any
// cluster (GKE/EKS/self-managed) with no apiserver OIDC configuration.
package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/kube-argus/kube-argus/service/internal/k8s"
	"github.com/kube-argus/kube-argus/service/internal/model"
)

// Verifier verifies a broker id_token.
type Verifier interface {
	Verify(ctx context.Context, rawIDToken string) (*oidc.IDToken, error)
}

// Minter mints a ServiceAccount token by name.
type Minter interface {
	Mint(ctx context.Context, saName string) (model.Token, error)
}

// Proxy authenticates incoming requests and forwards them to the apiserver.
type Proxy struct {
	verifier      Verifier
	minter        Minter
	usernameClaim string
	rp            *httputil.ReverseProxy
	cache         *tokenCache
	log           *slog.Logger
}

// New builds a Proxy that forwards to apiserver using transport (apiserver CA, no
// auth — the per-request SA token is set by the handler). cacheTTL is how long a
// minted SA token is reused before re-minting.
func New(verifier Verifier, minter Minter, apiserver *url.URL, transport http.RoundTripper,
	usernameClaim string, cacheTTL time.Duration, log *slog.Logger) *Proxy {
	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = apiserver.Scheme
			req.URL.Host = apiserver.Host
			req.Host = apiserver.Host
		},
		Transport:     transport,
		FlushInterval: -1, // flush immediately so `logs -f` / `exec` stream
	}
	return &Proxy{
		verifier: verifier, minter: minter, usernameClaim: usernameClaim,
		rp: rp, cache: newTokenCache(cacheTTL), log: log,
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw := bearer(r)

	// Unauthenticated /healthz for probes.
	if r.URL.Path == "/healthz" && raw == "" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}

	// Only broker id_tokens are swapped for a per-user SA token. Anything else
	// (e.g. the client's own ServiceAccount token, or an unauthenticated request)
	// passes through unchanged — the apiserver stays the authentication authority.
	if raw != "" {
		if idToken, err := p.verifier.Verify(r.Context(), raw); err == nil {
			if !p.swap(w, r, idToken) {
				return // swap attempted but failed (mint error already written)
			}
		}
	}
	p.rp.ServeHTTP(w, r)
}

// swap replaces the broker id_token with the user's SA token. Returns false (and
// writes an error) only when minting the SA token fails.
func (p *Proxy) swap(w http.ResponseWriter, r *http.Request, idToken *oidc.IDToken) bool {
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return true // malformed claims: pass the original token through
	}
	user, _ := claims[p.usernameClaim].(string)
	if user == "" {
		return true
	}
	saName := k8s.SanitizeName(user)
	saToken, err := p.cache.token(r.Context(), saName, p.minter)
	if err != nil {
		p.log.Error("obtain SA token", "sa", saName, "err", err)
		http.Error(w, "could not obtain credentials", http.StatusBadGateway)
		return false
	}
	// Replace the bearer and drop client impersonation headers (no spoofing).
	r.Header.Set("Authorization", "Bearer "+saToken)
	for h := range r.Header {
		if strings.HasPrefix(http.CanonicalHeaderKey(h), "Impersonate-") {
			r.Header.Del(h)
		}
	}
	return true
}

func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// tokenCache reuses a minted SA token per ServiceAccount until cacheTTL elapses.
//
// ponytail: no background eviction; entries are small and refreshed on read.
// Add a sweep if the set of users grows unbounded.
type tokenCache struct {
	mu  sync.Mutex
	m   map[string]cacheEntry
	ttl time.Duration
}

type cacheEntry struct {
	token string
	exp   time.Time
}

func newTokenCache(ttl time.Duration) *tokenCache {
	return &tokenCache{m: make(map[string]cacheEntry), ttl: ttl}
}

func (c *tokenCache) token(ctx context.Context, sa string, minter Minter) (string, error) {
	c.mu.Lock()
	if e, ok := c.m[sa]; ok && time.Now().Before(e.exp) {
		c.mu.Unlock()
		return e.token, nil
	}
	c.mu.Unlock()

	t, err := minter.Mint(ctx, sa)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.m[sa] = cacheEntry{token: t.AccessToken, exp: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return t.AccessToken, nil
}

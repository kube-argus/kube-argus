package broker

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kube-argus/kube-argus/service/internal/model"
	"github.com/kube-argus/kube-argus/service/internal/store"
)

const (
	authRequestTTL = 10 * time.Minute
	codeTTL        = 2 * time.Minute
)

// Discovery serves the OIDC provider metadata.
func (b *Broker) Discovery(w http.ResponseWriter, _ *http.Request) {
	iss := strings.TrimRight(b.cfg.Issuer, "/")
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                iss + "/authorize",
		"token_endpoint":                        iss + "/token",
		"jwks_uri":                              iss + "/jwks",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      []string{"openid", "email", "profile", "groups", "offline_access"},
		"claims_supported":                      []string{"sub", "email", "email_verified", "name", "groups"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"subject_types_supported":               []string{"public"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
	})
}

// JWKS serves the broker's id_token signing public keys.
func (b *Broker) JWKS(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, b.signer.jwks())
}

// Authorize is the broker's OIDC authorization endpoint (Headlamp -> broker).
func (b *Broker) Authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")

	// redirect_uri / client_id are validated first: never redirect to an unverified URI.
	if clientID != b.cfg.ClientID || !b.cfg.RedirectAllowed(redirectURI) {
		http.Error(w, "invalid client_id or redirect_uri", http.StatusBadRequest)
		return
	}

	state := q.Get("state")
	if q.Get("response_type") != "code" {
		redirectError(w, r, redirectURI, state, "unsupported_response_type", "only code is supported")
		return
	}
	if !scopeHas(q.Get("scope"), "openid") {
		redirectError(w, r, redirectURI, state, "invalid_scope", "openid scope required")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge == "" || q.Get("code_challenge_method") != "S256" {
		redirectError(w, r, redirectURI, state, "invalid_request", "PKCE S256 required")
		return
	}

	brokerState := randToken()
	idpNonce := randToken()
	ar := store.AuthRequest{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		State:               state,
		Nonce:               q.Get("nonce"),
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		Scope:               q.Get("scope"),
		IdPNonce:            idpNonce,
	}
	if err := b.store.SaveAuthRequest(r.Context(), brokerState, ar, authRequestTTL); err != nil {
		b.log.Error("save auth request", "err", err)
		redirectError(w, r, redirectURI, state, "server_error", "could not start login")
		return
	}
	http.Redirect(w, r, b.provider.AuthURL(brokerState, idpNonce), http.StatusFound)
}

// Callback handles Google's redirect: verify identity, bind, mint, issue code.
func (b *Broker) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	ar, err := b.store.TakeAuthRequest(ctx, q.Get("state"))
	if err != nil {
		http.Error(w, "unknown or expired login state", http.StatusBadRequest)
		return
	}

	if e := q.Get("error"); e != "" {
		redirectError(w, r, ar.RedirectURI, ar.State, e, q.Get("error_description"))
		return
	}

	id, err := b.provider.Authenticate(ctx, q.Get("code"), ar.IdPNonce)
	if err != nil {
		b.log.Warn("idp authentication failed", "err", err)
		redirectError(w, r, ar.RedirectURI, ar.State, "access_denied", "authentication failed")
		return
	}

	// Policy: verified email within an allowed domain.
	if !id.EmailVerified || !b.cfg.AllowedDomain(id.Domain) {
		b.log.Warn("policy rejected identity", "email", id.Email, "domain", id.Domain, "verified", id.EmailVerified)
		redirectError(w, r, ar.RedirectURI, ar.State, "access_denied", "domain or email not allowed")
		return
	}

	tokens, err := b.issueTokens(ctx, id, ar.ClientID, ar.Nonce)
	if err != nil {
		b.log.Error("issue tokens", "err", err)
		redirectError(w, r, ar.RedirectURI, ar.State, "server_error", "could not issue tokens")
		return
	}

	code := randToken()
	grant := store.CodeGrant{
		ClientID:      ar.ClientID,
		RedirectURI:   ar.RedirectURI,
		CodeChallenge: ar.CodeChallenge,
		Nonce:         ar.Nonce,
		Subject:       id.Email,
		AccessToken:   tokens.access,  // SA token: bearer for the kube-apiserver
		IDToken:       tokens.id,      // broker-signed JWT: for the client's OIDC verify
		RefreshToken:  tokens.refresh, // exchanged later for fresh tokens
		ExpiresIn:     tokens.expiresIn,
	}
	if err := b.store.SaveCode(ctx, code, grant, codeTTL); err != nil {
		b.log.Error("save code", "err", err)
		redirectError(w, r, ar.RedirectURI, ar.State, "server_error", "could not issue code")
		return
	}

	u, _ := url.Parse(ar.RedirectURI)
	rq := u.Query()
	rq.Set("code", code)
	if ar.State != "" {
		rq.Set("state", ar.State)
	}
	u.RawQuery = rq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// issued bundles the three tokens returned to the client.
type issued struct {
	access    string
	id        string
	refresh   string
	expiresIn int
}

// issueTokens re-binds the identity, mints a fresh SA token, signs an id_token,
// and stores a rotating refresh token. Shared by the code and refresh grants.
func (b *Broker) issueTokens(ctx context.Context, id model.Identity, clientID, nonce string) (issued, error) {
	name, err := b.binder.Upsert(ctx, id)
	if err != nil {
		return issued{}, fmt.Errorf("upsert bind: %w", err)
	}
	if err := b.binder.WaitBinded(ctx, name); err != nil {
		return issued{}, fmt.Errorf("wait binded: %w", err)
	}
	tok, err := b.minter.Mint(ctx, name)
	if err != nil {
		return issued{}, fmt.Errorf("mint token: %w", err)
	}
	sub := id.Subject
	if sub == "" {
		sub = id.Email
	}
	idToken, err := b.signer.signIDToken(idClaims{
		Sub: sub, Aud: clientID, Nonce: nonce,
		Email: id.Email, EmailVerified: id.EmailVerified, Name: id.Name,
		Groups: gids(id.Groups),
	}, b.cfg.TokenLifetime)
	if err != nil {
		return issued{}, fmt.Errorf("sign id_token: %w", err)
	}
	refresh := randToken()
	rg := store.RefreshGrant{
		ClientID: clientID, Email: id.Email, Subject: id.Subject, Name: id.Name,
		EmailVerified: id.EmailVerified, Domain: id.Domain, Groups: toGroupRefs(id.Groups),
	}
	if err := b.store.SaveRefresh(ctx, refresh, rg, b.cfg.RefreshTokenLifetime); err != nil {
		return issued{}, fmt.Errorf("save refresh: %w", err)
	}
	return issued{access: tok.AccessToken, id: idToken, refresh: refresh, expiresIn: tok.ExpiresIn}, nil
}

// Token is the broker's OIDC token endpoint (client -> broker).
func (b *Broker) Token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		b.log.Warn("token: bad form", "err", err)
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "bad form")
		return
	}
	switch gt := r.PostForm.Get("grant_type"); gt {
	case "authorization_code":
		b.handleAuthCode(w, r)
	case "refresh_token":
		b.handleRefresh(w, r)
	default:
		b.log.Warn("token: unsupported grant_type", "grant_type", gt)
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "")
	}
}

// clientID resolves the client id from the form body or HTTP Basic auth.
func clientIDOf(r *http.Request) string {
	if c := r.PostForm.Get("client_id"); c != "" {
		return c
	}
	if u, _, ok := r.BasicAuth(); ok {
		return u
	}
	return ""
}

func (b *Broker) handleAuthCode(w http.ResponseWriter, r *http.Request) {
	grant, err := b.store.TakeCode(r.Context(), r.PostForm.Get("code"))
	if err != nil {
		b.log.Warn("token: code not found", "err", err)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "unknown or used code")
		return
	}
	if clientIDOf(r) != grant.ClientID || r.PostForm.Get("redirect_uri") != grant.RedirectURI {
		b.log.Warn("token: client/redirect mismatch", "client_id", clientIDOf(r))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client or redirect mismatch")
		return
	}
	if b.cfg.ClientSecret != "" && !b.validClientSecret(r) {
		b.log.Warn("token: bad client credentials")
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "bad client credentials")
		return
	}
	if !verifyPKCE(r.PostForm.Get("code_verifier"), grant.CodeChallenge) {
		b.log.Warn("token: PKCE verification failed")
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}
	writeTokenResponse(w, grant.AccessToken, grant.IDToken, grant.RefreshToken, grant.ExpiresIn)
}

func (b *Broker) handleRefresh(w http.ResponseWriter, r *http.Request) {
	rg, err := b.store.TakeRefresh(r.Context(), r.PostForm.Get("refresh_token"))
	if err != nil {
		b.log.Warn("token: refresh token not found", "err", err)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "unknown or used refresh token")
		return
	}
	if clientIDOf(r) != rg.ClientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client mismatch")
		return
	}
	if b.cfg.ClientSecret != "" && !b.validClientSecret(r) {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "bad client credentials")
		return
	}
	id := model.Identity{
		Email: rg.Email, Subject: rg.Subject, Name: rg.Name,
		EmailVerified: rg.EmailVerified, Domain: rg.Domain, Groups: fromGroupRefs(rg.Groups),
	}
	tokens, err := b.issueTokens(r.Context(), id, rg.ClientID, "")
	if err != nil {
		b.log.Error("refresh: issue tokens", "err", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not refresh")
		return
	}
	writeTokenResponse(w, tokens.access, tokens.id, tokens.refresh, tokens.expiresIn)
}

func writeTokenResponse(w http.ResponseWriter, access, id, refresh string, expiresIn int) {
	resp := map[string]any{
		"access_token": access,
		"id_token":     id,
		"token_type":   "Bearer",
		"expires_in":   expiresIn,
		"scope":        "openid",
	}
	if refresh != "" {
		resp["refresh_token"] = refresh
	}
	writeJSON(w, http.StatusOK, resp)
}

// Healthz is a liveness/readiness probe.
func (b *Broker) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (b *Broker) validClientSecret(r *http.Request) bool {
	secret := r.PostForm.Get("client_secret")
	if secret == "" {
		if _, pw, ok := r.BasicAuth(); ok {
			secret = pw
		}
	}
	return constantEq(secret, b.cfg.ClientSecret)
}

func redirectError(w http.ResponseWriter, r *http.Request, redirectURI, state, code, desc string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, code+": "+desc, http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("error", code)
	if desc != "" {
		q.Set("error_description", desc)
	}
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// gids extracts the group ids from memberships for the id_token groups claim.
func gids(ms []model.Membership) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Gid)
	}
	return out
}

func toGroupRefs(ms []model.Membership) []store.GroupRef {
	out := make([]store.GroupRef, 0, len(ms))
	for _, m := range ms {
		out = append(out, store.GroupRef{Gid: m.Gid, Name: m.Name, Domain: m.Domain})
	}
	return out
}

func fromGroupRefs(gs []store.GroupRef) []model.Membership {
	out := make([]model.Membership, 0, len(gs))
	for _, g := range gs {
		out = append(out, model.Membership{Gid: g.Gid, Name: g.Name, Domain: g.Domain})
	}
	return out
}

func scopeHas(scope, want string) bool {
	for _, s := range strings.Fields(scope) {
		if s == want {
			return true
		}
	}
	return false
}

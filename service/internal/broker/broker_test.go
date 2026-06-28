package broker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kube-argos/kargos/service/internal/config"
	"github.com/kube-argos/kargos/service/internal/model"
	"github.com/kube-argos/kargos/service/internal/store"
)

// --- fakes implementing the broker dependencies ---

type fakeProvider struct {
	id        model.Identity
	err       error
	lastNonce string
}

func (f *fakeProvider) AuthURL(state, nonce string) string {
	return "https://idp.example/authorize?state=" + state + "&nonce=" + nonce
}
func (f *fakeProvider) Authenticate(_ context.Context, _, nonce string) (model.Identity, error) {
	f.lastNonce = nonce
	return f.id, f.err
}

type fakeBinder struct {
	name    string
	waitErr error
}

func (f fakeBinder) Upsert(context.Context, model.Identity) (string, error) {
	return f.name, nil
}
func (f fakeBinder) WaitBinded(context.Context, string) error { return f.waitErr }

type fakeMinter struct{ tok model.Token }

func (f fakeMinter) Mint(context.Context, string) (model.Token, error) { return f.tok, nil }

func testBroker(provider Provider, binder Binder) (*Broker, store.Store) {
	cfg := &config.Config{
		Issuer:               "https://broker.example",
		ClientID:             "dashboard",
		RedirectURIs:         []string{"https://client.example/oidc/callback"},
		AllowedDomains:       []string{"golinux.network"},
		TokenLifetime:        time.Hour,
		RefreshTokenLifetime: time.Hour,
	}
	st, err := store.New(config.StoreConfig{Backend: "memory"})
	if err != nil {
		panic(err)
	}
	b, err := New(cfg, st, provider, binder,
		fakeMinter{tok: model.Token{AccessToken: "SA-TOKEN", ExpiresIn: 900}},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		panic(err)
	}
	return b, st
}

func pkcePair() (verifier, challenge string) {
	verifier = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestVerifyPKCE(t *testing.T) {
	v, ch := pkcePair()
	if !verifyPKCE(v, ch) {
		t.Fatal("expected PKCE to verify")
	}
	if verifyPKCE("wrong", ch) {
		t.Fatal("expected mismatched verifier to fail")
	}
}

func TestScopeHas(t *testing.T) {
	if !scopeHas("openid email", "openid") {
		t.Fatal("expected openid present")
	}
	if scopeHas("email profile", "openid") {
		t.Fatal("expected openid absent")
	}
}

func queryOf(t *testing.T, location string) url.Values {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse location %q: %v", location, err)
	}
	return u.Query()
}

// TestFullFlow drives authorize -> callback -> token end to end with fakes.
func TestFullFlow(t *testing.T) {
	provider := &fakeProvider{id: model.Identity{Email: "lucas@golinux.network", EmailVerified: true, Domain: "golinux.network"}}
	b, st := testBroker(provider, fakeBinder{name: "lucas"})
	verifier, challenge := pkcePair()

	// 1. /authorize
	authURL := "/authorize?response_type=code&client_id=dashboard" +
		"&redirect_uri=" + url.QueryEscape("https://client.example/oidc/callback") +
		"&scope=openid&state=hl-state&nonce=hl-nonce" +
		"&code_challenge=" + challenge + "&code_challenge_method=S256"
	rec := httptest.NewRecorder()
	b.Authorize(rec, httptest.NewRequest(http.MethodGet, authURL, nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize status = %d", rec.Code)
	}
	gq := queryOf(t, rec.Header().Get("Location"))
	brokerState := gq.Get("state")
	if brokerState == "" || gq.Get("nonce") == "" {
		t.Fatal("expected broker state + nonce in google redirect")
	}

	// 2. /callback
	rec = httptest.NewRecorder()
	b.Callback(rec, httptest.NewRequest(http.MethodGet, "/callback?state="+brokerState+"&code=google-code", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("callback status = %d (%s)", rec.Code, rec.Body.String())
	}
	cq := queryOf(t, rec.Header().Get("Location"))
	if cq.Get("state") != "hl-state" {
		t.Fatalf("expected echoed state hl-state, got %q", cq.Get("state"))
	}
	code := cq.Get("code")
	if code == "" {
		t.Fatal("expected authorization code")
	}
	if provider.lastNonce == "" {
		t.Fatal("expected google nonce to be passed to Exchange")
	}

	// 3. /token
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"https://client.example/oidc/callback"},
		"client_id":     {"dashboard"},
		"code_verifier": {verifier},
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	b.Token(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("token status = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["access_token"] != "SA-TOKEN" || resp["token_type"] != "Bearer" {
		t.Fatalf("unexpected token response: %v", resp)
	}

	// 4. replay: code is one-shot.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	b.Token(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("expected replayed code to be rejected")
	}
	_ = st
}

func TestCallback_PolicyRejectsForeignDomain(t *testing.T) {
	provider := &fakeProvider{id: model.Identity{Email: "x@evil.com", EmailVerified: true, Domain: "evil.com"}}
	b, _ := testBroker(provider, fakeBinder{name: "x"})

	// seed an auth request
	ctx := context.Background()
	_ = b.store.SaveAuthRequest(ctx, "bs", store.AuthRequest{
		ClientID: "dashboard", RedirectURI: "https://client.example/oidc/callback", State: "hl",
	}, authRequestTTL)

	rec := httptest.NewRecorder()
	b.Callback(rec, httptest.NewRequest(http.MethodGet, "/callback?state=bs&code=c", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := queryOf(t, rec.Header().Get("Location")).Get("error"); got != "access_denied" {
		t.Fatalf("error = %q, want access_denied", got)
	}
}

func TestToken_BadPKCE(t *testing.T) {
	b, _ := testBroker(&fakeProvider{}, fakeBinder{})
	_, challenge := pkcePair()
	_ = b.store.SaveCode(context.Background(), "code", store.CodeGrant{
		ClientID: "dashboard", RedirectURI: "https://client.example/oidc/callback", CodeChallenge: challenge,
	}, codeTTL)

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"code"},
		"redirect_uri":  {"https://client.example/oidc/callback"},
		"client_id":     {"dashboard"},
		"code_verifier": {"the-wrong-verifier"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	b.Token(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRefreshGrant_RotatesAndRebinds(t *testing.T) {
	b, st := testBroker(&fakeProvider{}, fakeBinder{name: "lucas"})
	_ = st.SaveRefresh(context.Background(), "rt1", store.RefreshGrant{
		ClientID: "dashboard", Email: "lucas@golinux.network", Domain: "golinux.network",
		EmailVerified: true, Groups: []store.GroupRef{{Gid: "g/eng"}},
	}, time.Hour)

	refresh := func(token string) *httptest.ResponseRecorder {
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token}, "client_id": {"dashboard"}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		b.Token(rec, req)
		return rec
	}

	rec := refresh("rt1")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["access_token"] != "SA-TOKEN" {
		t.Fatalf("access_token = %v", resp["access_token"])
	}
	newRT, _ := resp["refresh_token"].(string)
	if newRT == "" || newRT == "rt1" {
		t.Fatalf("expected a rotated refresh_token, got %q", newRT)
	}

	// rt1 is one-shot: reuse must fail.
	if rec := refresh("rt1"); rec.Code == http.StatusOK {
		t.Fatal("expected reused refresh token to be rejected")
	}
	// the rotated token works.
	if rec := refresh(newRT); rec.Code != http.StatusOK {
		t.Fatalf("rotated refresh failed: %d", rec.Code)
	}
}

func TestAuthorize_RejectsUnknownRedirect(t *testing.T) {
	b, _ := testBroker(&fakeProvider{}, fakeBinder{})
	rec := httptest.NewRecorder()
	b.Authorize(rec, httptest.NewRequest(http.MethodGet,
		"/authorize?response_type=code&client_id=dashboard&redirect_uri=https://evil.example/cb&scope=openid&code_challenge=x&code_challenge_method=S256", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for bad redirect_uri", rec.Code)
	}
}

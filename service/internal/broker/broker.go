// Package broker is the OpenID Provider the broker exposes to Headlamp. It
// federates to Google, drives the UserAuthenticationBind lifecycle, and returns
// a minted ServiceAccount token.
package broker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kube-argos/kargos/service/internal/config"
	"github.com/kube-argos/kargos/service/internal/model"
	"github.com/kube-argos/kargos/service/internal/store"
)

// Provider is the upstream IdP: OIDC login plus group resolution.
type Provider interface {
	AuthURL(state, nonce string) string
	Authenticate(ctx context.Context, code, nonce string) (model.Identity, error)
}

// Binder upserts the CR and waits for the operator to bind it.
type Binder interface {
	Upsert(ctx context.Context, id model.Identity) (string, error)
	WaitBinded(ctx context.Context, name string) error
}

// Minter mints a ServiceAccount token.
type Minter interface {
	Mint(ctx context.Context, saName string) (model.Token, error)
}

// Broker wires the OP handlers to their dependencies.
type Broker struct {
	cfg      *config.Config
	store    store.Store
	provider Provider
	binder   Binder
	minter   Minter
	signer   *signer
	log      *slog.Logger
}

// New constructs a Broker, building the id_token signer from config.
func New(cfg *config.Config, st store.Store, provider Provider, binder Binder, minter Minter, log *slog.Logger) (*Broker, error) {
	sg, generated, err := newSigner(strings.TrimRight(cfg.Issuer, "/"), cfg.IDTokenSigningKey)
	if err != nil {
		return nil, fmt.Errorf("id_token signer: %w", err)
	}
	if generated {
		log.Warn("ID_TOKEN_SIGNING_KEY not set: using an ephemeral signing key " +
			"(tokens break on restart and across replicas — set a key for production)")
	}
	return &Broker{cfg: cfg, store: st, provider: provider, binder: binder, minter: minter, signer: sg, log: log}, nil
}

// randToken returns a 256-bit URL-safe random token.
func randToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// verifyPKCE checks an S256 PKCE code_verifier against the stored challenge.
func verifyPKCE(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

// constantEq compares two secrets in constant time.
func constantEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

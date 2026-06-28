// Package store persists short-lived broker state (pending auth requests and
// issued authorization codes) behind a backend-agnostic interface.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/kube-argos/kargos/service/internal/config"
)

// ErrNotFound is returned when a key is absent or expired.
var ErrNotFound = errors.New("store: not found")

// AuthRequest is a pending Headlamp->broker authorization, keyed by the broker
// state sent to Google. Recovered when Google redirects back to /callback.
type AuthRequest struct {
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	State               string `json:"state"`         // Headlamp's state, echoed back
	Nonce               string `json:"nonce"`         // Headlamp's nonce
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	Scope               string `json:"scope"`
	IdPNonce            string `json:"idp_nonce"` // nonce sent to the IdP, verified on callback
}

// CodeGrant is the broker authorization code handed to the client, exchanged
// once at /token for the minted ServiceAccount token.
type CodeGrant struct {
	ClientID      string `json:"client_id"`
	RedirectURI   string `json:"redirect_uri"`
	CodeChallenge string `json:"code_challenge"`
	Nonce         string `json:"nonce"`
	Subject       string `json:"subject"`
	AccessToken   string `json:"access_token"` // the SA token
	IDToken       string `json:"id_token"`
	RefreshToken  string `json:"refresh_token"`
	ExpiresIn     int    `json:"expires_in"`
}

// GroupRef is a stored group membership (avoids importing the model package).
type GroupRef struct {
	Gid    string `json:"gid"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
}

// RefreshGrant is the long-lived state behind a refresh_token: enough identity
// to re-bind the user and mint fresh tokens without another IdP round-trip.
type RefreshGrant struct {
	ClientID      string     `json:"client_id"`
	Email         string     `json:"email"`
	Subject       string     `json:"subject"`
	Name          string     `json:"name"`
	EmailVerified bool       `json:"email_verified"`
	Domain        string     `json:"domain"`
	Groups        []GroupRef `json:"groups"`
}

// Store is the persistence contract. Implementations must make TakeCode atomic
// (one-shot) so an authorization code cannot be replayed across replicas.
type Store interface {
	SaveAuthRequest(ctx context.Context, key string, ar AuthRequest, ttl time.Duration) error
	TakeAuthRequest(ctx context.Context, key string) (AuthRequest, error)
	SaveCode(ctx context.Context, code string, g CodeGrant, ttl time.Duration) error
	TakeCode(ctx context.Context, code string) (CodeGrant, error)
	SaveRefresh(ctx context.Context, token string, g RefreshGrant, ttl time.Duration) error
	// TakeRefresh is one-shot (rotation): returns + deletes atomically.
	TakeRefresh(ctx context.Context, token string) (RefreshGrant, error)
	Close() error
}

// New builds the configured store backend.
func New(cfg config.StoreConfig) (Store, error) {
	switch cfg.Backend {
	case "redis":
		return newRedis(cfg)
	default:
		return newMemory(), nil
	}
}

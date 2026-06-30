// Package idp abstracts the upstream identity provider: any IdP that can do the
// OIDC authorization-code flow and report group membership.
package idp

import (
	"context"
	"fmt"

	"github.com/kube-argus/kube-argus/service/internal/config"
	"github.com/kube-argus/kube-argus/service/internal/idp/google"
	"github.com/kube-argus/kube-argus/service/internal/idp/oidc"
	"github.com/kube-argus/kube-argus/service/internal/model"
)

// Provider is an external IdP capable of OIDC login and group resolution.
type Provider interface {
	// AuthURL returns the IdP authorization URL for the given state and nonce.
	AuthURL(state, nonce string) string
	// Authenticate exchanges the callback code and returns the verified identity,
	// including group memberships.
	Authenticate(ctx context.Context, code, nonce string) (model.Identity, error)
}

// New builds the configured IdP provider.
func New(ctx context.Context, cfg *config.Config) (Provider, error) {
	switch cfg.IdP.Type {
	case "google":
		return google.New(ctx, cfg)
	case "oidc":
		return oidc.New(ctx, cfg)
	default:
		return nil, fmt.Errorf("unknown IDP_TYPE %q", cfg.IdP.Type)
	}
}

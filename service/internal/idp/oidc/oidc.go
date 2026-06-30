// Package oidc is a generic OpenID Connect IdP provider. It works with any
// compliant issuer (Keycloak, Okta, Entra, Authentik, Dex, ...) and reads group
// membership from a configurable ID-token claim.
package oidc

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/kube-argus/kube-argus/service/internal/config"
	"github.com/kube-argus/kube-argus/service/internal/model"
)

// Provider implements idp.Provider over a generic OIDC issuer.
type Provider struct {
	oauth       *oauth2.Config
	verifier    *oidc.IDTokenVerifier
	groupsClaim string
}

// New performs OIDC discovery and builds the provider.
func New(ctx context.Context, cfg *config.Config) (*Provider, error) {
	provider, err := oidc.NewProvider(ctx, cfg.IdP.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery (%s): %w", cfg.IdP.Issuer, err)
	}
	return &Provider{
		oauth: &oauth2.Config{
			ClientID:     cfg.IdP.ClientID,
			ClientSecret: cfg.IdP.ClientSecret,
			RedirectURL:  cfg.IdP.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       cfg.IdP.Scopes,
		},
		verifier:    provider.Verifier(&oidc.Config{ClientID: cfg.IdP.ClientID}),
		groupsClaim: cfg.IdP.GroupsClaim,
	}, nil
}

// AuthURL builds the authorization URL.
func (p *Provider) AuthURL(state, nonce string) string {
	return p.oauth.AuthCodeURL(state, oidc.Nonce(nonce))
}

// Authenticate exchanges the code, verifies the ID token, and extracts identity
// plus groups from the configured claim.
func (p *Provider) Authenticate(ctx context.Context, code, nonce string) (model.Identity, error) {
	tok, err := p.oauth.Exchange(ctx, code)
	if err != nil {
		return model.Identity{}, fmt.Errorf("code exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return model.Identity{}, fmt.Errorf("response missing id_token")
	}
	idToken, err := p.verifier.Verify(ctx, rawID)
	if err != nil {
		return model.Identity{}, fmt.Errorf("verify id_token: %w", err)
	}
	if nonce != "" && idToken.Nonce != nonce {
		return model.Identity{}, fmt.Errorf("id_token nonce mismatch")
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return model.Identity{}, fmt.Errorf("parse claims: %w", err)
	}
	email, _ := claims["email"].(string)
	id := model.Identity{
		Subject:       idToken.Subject,
		Email:         email,
		EmailVerified: asBool(claims["email_verified"]),
		Name:          asString(claims["name"]),
		Domain:        DomainOf(email),
	}
	id.Groups = ParseGroups(claims[p.groupsClaim], id.Domain)
	return id, nil
}

// ParseGroups extracts group ids from a claim value (a JSON array of strings).
func ParseGroups(claim any, domain string) []model.Membership {
	raw, ok := claim.([]any)
	if !ok {
		return nil
	}
	out := make([]model.Membership, 0, len(raw))
	for _, v := range raw {
		if g, ok := v.(string); ok && g != "" {
			out = append(out, model.Membership{Gid: g, Name: g, Domain: domain})
		}
	}
	return out
}

// DomainOf returns the domain part of an email address.
func DomainOf(email string) string {
	for i := len(email) - 1; i >= 0; i-- {
		if email[i] == '@' {
			return email[i+1:]
		}
	}
	return ""
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

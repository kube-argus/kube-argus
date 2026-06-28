// Package google is the Google Workspace IdP provider: OIDC login plus group
// membership from the Admin SDK Directory API, using the "hd" claim as the domain.
package google

import (
	"context"
	"fmt"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	directory "google.golang.org/api/admin/directory/v1"
	"google.golang.org/api/option"

	"github.com/kube-argos/kargos/service/internal/config"
	"github.com/kube-argos/kargos/service/internal/model"
)

// Provider implements idp.Provider for Google Workspace.
type Provider struct {
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
	dir      *directory.Service
}

// New performs OIDC discovery and builds the Directory API client (domain-wide
// delegation impersonating the configured admin).
func New(ctx context.Context, cfg *config.Config) (*Provider, error) {
	provider, err := oidc.NewProvider(ctx, cfg.IdP.Issuer)
	if err != nil {
		return nil, fmt.Errorf("google oidc discovery: %w", err)
	}

	dir, err := newDirectory(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return &Provider{
		oauth: &oauth2.Config{
			ClientID:     cfg.IdP.ClientID,
			ClientSecret: cfg.IdP.ClientSecret,
			RedirectURL:  cfg.IdP.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       cfg.IdP.Scopes,
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.IdP.ClientID}),
		dir:      dir,
	}, nil
}

// newDirectory builds an Admin SDK Directory client using Google Workspace
// domain-wide delegation: a JWT signed by the service-account key with the
// "subject" set to the admin to impersonate. (option.ImpersonateCredentials is
// SA->SA impersonation and does NOT perform domain-wide delegation.)
func newDirectory(ctx context.Context, cfg *config.Config) (*directory.Service, error) {
	key, err := serviceAccountKey(ctx, cfg)
	if err != nil {
		return nil, err
	}
	jwtCfg, err := google.JWTConfigFromJSON(key, directory.AdminDirectoryGroupReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("google domain-wide delegation needs a service-account key: %w", err)
	}
	// The admin whose Workspace directory we read (must be a super-admin there).
	jwtCfg.Subject = cfg.IdP.GoogleDelegatedAdmin
	return directory.NewService(ctx, option.WithTokenSource(jwtCfg.TokenSource(ctx)))
}

// serviceAccountKey returns the SA key JSON from the configured file, or from
// Application Default Credentials when no file is set.
func serviceAccountKey(ctx context.Context, cfg *config.Config) ([]byte, error) {
	if cfg.IdP.GoogleCredentialsFile != "" {
		b, err := os.ReadFile(cfg.IdP.GoogleCredentialsFile)
		if err != nil {
			return nil, fmt.Errorf("read GOOGLE_CREDENTIALS_FILE: %w", err)
		}
		return b, nil
	}
	creds, err := google.FindDefaultCredentials(ctx, directory.AdminDirectoryGroupReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("no google credentials (set GOOGLE_CREDENTIALS_FILE): %w", err)
	}
	if len(creds.JSON) == 0 {
		return nil, fmt.Errorf("domain-wide delegation requires a service-account key file; " +
			"set GOOGLE_CREDENTIALS_FILE (metadata/Workload-Identity ADC has no key to sign the subject JWT)")
	}
	return creds.JSON, nil
}

// AuthURL builds the authorization URL.
func (p *Provider) AuthURL(state, nonce string) string {
	return p.oauth.AuthCodeURL(state, oidc.Nonce(nonce))
}

// Authenticate exchanges the code, verifies the ID token (hd as domain), and
// loads groups from the Directory API.
func (p *Provider) Authenticate(ctx context.Context, code, nonce string) (model.Identity, error) {
	tok, err := p.oauth.Exchange(ctx, code)
	if err != nil {
		return model.Identity{}, fmt.Errorf("google code exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return model.Identity{}, fmt.Errorf("google response missing id_token")
	}
	idToken, err := p.verifier.Verify(ctx, rawID)
	if err != nil {
		return model.Identity{}, fmt.Errorf("verify google id_token: %w", err)
	}
	if nonce != "" && idToken.Nonce != nonce {
		return model.Identity{}, fmt.Errorf("google id_token nonce mismatch")
	}
	var claims struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		HostedDomain  string `json:"hd"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return model.Identity{}, fmt.Errorf("parse google claims: %w", err)
	}

	groups, err := p.fetchGroups(ctx, claims.Email, claims.HostedDomain)
	if err != nil {
		return model.Identity{}, err
	}
	return model.Identity{
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		Domain:        claims.HostedDomain,
		Name:          claims.Name,
		Groups:        groups,
	}, nil
}

func (p *Provider) fetchGroups(ctx context.Context, email, domain string) ([]model.Membership, error) {
	var out []model.Membership
	call := p.dir.Groups.List().UserKey(email).MaxResults(200)
	err := call.Pages(ctx, func(page *directory.Groups) error {
		for _, g := range page.Groups {
			out = append(out, model.Membership{Gid: g.Id, Name: g.Name, Domain: domain})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list groups for %s: %w", email, err)
	}
	return out, nil
}

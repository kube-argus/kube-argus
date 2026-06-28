// Package config loads broker configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully-resolved broker configuration.
type Config struct {
	ListenAddr string
	Issuer     string // broker's own public URL (OIDC issuer)

	// IdP is the upstream identity provider (broker is the RP).
	IdP IdPConfig

	AllowedDomains []string // email/hd domain allowlist

	// The broker's own OIDC client (the thing logging in, e.g. a dashboard).
	ClientID      string
	ClientSecret  string // optional; empty => public client (PKCE only)
	RedirectURIs         []string
	TokenLifetime        time.Duration
	RefreshTokenLifetime time.Duration

	// IDTokenSigningKey is the RSA private key (PEM) used to sign broker id_tokens.
	// Empty => an ephemeral key is generated at startup (dev only; breaks HA and
	// invalidates tokens on restart).
	IDTokenSigningKey string

	// TLSCertFile / TLSKeyFile, when both set, make the broker serve HTTPS
	// directly (terminate TLS in-process). Empty => plain HTTP (TLS at the ingress).
	TLSCertFile string
	TLSKeyFile  string

	// UserAuthenticationBind CR.
	BindNamespace string
	BindTTL       string

	// Token audience requested from the kube-apiserver TokenRequest.
	TokenAudiences []string

	// Session/code store.
	Store StoreConfig
}

// IdPConfig configures the upstream IdP. Type selects the implementation.
type IdPConfig struct {
	Type         string // "oidc" (generic) | "google"
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string // broker /callback, registered with the IdP
	Scopes       []string

	// Generic OIDC: the ID-token claim that carries group membership.
	GroupsClaim string

	// Google only: domain-wide delegation for the Directory API.
	GoogleDelegatedAdmin  string
	GoogleCredentialsFile string
}

// StoreConfig selects and configures the session/code backend.
type StoreConfig struct {
	Backend       string // "memory" | "redis"
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisTLS      bool
}

// Load reads configuration from the environment, applies defaults, and validates.
func Load() (*Config, error) {
	c := &Config{
		ListenAddr:     env("LISTEN_ADDR", ":8080"),
		Issuer:         os.Getenv("ISSUER"),
		AllowedDomains: splitCSV(os.Getenv("ALLOWED_DOMAINS")),
		ClientID:       os.Getenv("CLIENT_ID"),
		ClientSecret:   os.Getenv("CLIENT_SECRET"),
		RedirectURIs:      splitCSV(os.Getenv("REDIRECT_URIS")),
		TokenLifetime:     time.Duration(envInt("TOKEN_LIFETIME_SECONDS", 900)) * time.Second,
		IDTokenSigningKey: os.Getenv("ID_TOKEN_SIGNING_KEY"),
		TLSCertFile:          os.Getenv("TLS_CERT_FILE"),
		TLSKeyFile:           os.Getenv("TLS_KEY_FILE"),
		BindNamespace:  env("BIND_NAMESPACE", "kargus-system"),
		BindTTL:        env("BIND_TTL", "12h"),
		// Empty => mint the SA token with no explicit audience, so it is bound to
		// the apiserver's default --api-audiences (which natively accepts it).
		// Only set this when the token is consumed by something other than the
		// local kube-apiserver.
		TokenAudiences: splitCSV(os.Getenv("TOKEN_AUDIENCES")),
		IdP: IdPConfig{
			Type:         env("IDP_TYPE", "oidc"),
			Issuer:       os.Getenv("IDP_ISSUER"),
			ClientID:     os.Getenv("IDP_CLIENT_ID"),
			ClientSecret: os.Getenv("IDP_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("IDP_REDIRECT_URL"),
			Scopes:       splitCSV(env("IDP_SCOPES", "openid,email,profile")),
			GroupsClaim:  env("IDP_GROUPS_CLAIM", "groups"),

			GoogleDelegatedAdmin:  os.Getenv("GOOGLE_DELEGATED_ADMIN"),
			GoogleCredentialsFile: os.Getenv("GOOGLE_CREDENTIALS_FILE"),
		},
		Store: StoreConfig{
			Backend:       env("SESSION_STORE", "memory"),
			RedisAddr:     os.Getenv("REDIS_ADDR"),
			RedisPassword: os.Getenv("REDIS_PASSWORD"),
			RedisDB:       envInt("REDIS_DB", 0),
			RedisTLS:      envBool("REDIS_TLS", false),
		},
	}
	// Google has a fixed issuer; default it so operators need not set IDP_ISSUER.
	if c.IdP.Type == "google" && c.IdP.Issuer == "" {
		c.IdP.Issuer = "https://accounts.google.com"
	}

	// Session length = refresh-token lifetime. Default it to BIND_TTL so the
	// session and the UserAuthenticationBind share the same sliding window: each
	// refresh renews both, and they expire together. Override explicitly with
	// REFRESH_TOKEN_LIFETIME_SECONDS.
	switch {
	case os.Getenv("REFRESH_TOKEN_LIFETIME_SECONDS") != "":
		c.RefreshTokenLifetime = time.Duration(envInt("REFRESH_TOKEN_LIFETIME_SECONDS", 28800)) * time.Second
	default:
		if d, err := time.ParseDuration(c.BindTTL); err == nil {
			c.RefreshTokenLifetime = d
		} else {
			c.RefreshTokenLifetime = 8 * time.Hour
		}
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	var missing []string
	require := func(name, val string) {
		if val == "" {
			missing = append(missing, name)
		}
	}
	require("ISSUER", c.Issuer)
	require("IDP_ISSUER", c.IdP.Issuer)
	require("IDP_CLIENT_ID", c.IdP.ClientID)
	require("IDP_CLIENT_SECRET", c.IdP.ClientSecret)
	require("IDP_REDIRECT_URL", c.IdP.RedirectURL)
	require("CLIENT_ID", c.ClientID)
	if len(c.RedirectURIs) == 0 {
		missing = append(missing, "REDIRECT_URIS")
	}
	if len(c.AllowedDomains) == 0 {
		missing = append(missing, "ALLOWED_DOMAINS")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}

	switch c.IdP.Type {
	case "oidc":
		if c.IdP.GroupsClaim == "" {
			return fmt.Errorf("IDP_TYPE=oidc requires IDP_GROUPS_CLAIM")
		}
	case "google":
		if c.IdP.GoogleDelegatedAdmin == "" {
			return fmt.Errorf("IDP_TYPE=google requires GOOGLE_DELEGATED_ADMIN")
		}
	default:
		return fmt.Errorf("IDP_TYPE must be oidc or google, got %q", c.IdP.Type)
	}

	switch c.Store.Backend {
	case "memory":
	case "redis":
		if c.Store.RedisAddr == "" {
			return fmt.Errorf("SESSION_STORE=redis requires REDIS_ADDR")
		}
	default:
		return fmt.Errorf("SESSION_STORE must be memory or redis, got %q", c.Store.Backend)
	}
	return nil
}

// AllowedDomain reports whether domain is in the allowlist.
func (c *Config) AllowedDomain(domain string) bool {
	for _, d := range c.AllowedDomains {
		if strings.EqualFold(d, domain) {
			return true
		}
	}
	return false
}

// RedirectAllowed reports whether uri exactly matches a registered redirect URI.
func (c *Config) RedirectAllowed(uri string) bool {
	for _, r := range c.RedirectURIs {
		if r == uri {
			return true
		}
	}
	return false
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

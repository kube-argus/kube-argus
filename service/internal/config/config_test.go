package config

import "testing"

func TestAllowedDomain(t *testing.T) {
	c := &Config{AllowedDomains: []string{"kargus.io"}}
	if !c.AllowedDomain("Kargus.IO") {
		t.Fatal("expected case-insensitive match")
	}
	if c.AllowedDomain("evil.com") {
		t.Fatal("expected non-match")
	}
}

func TestRedirectAllowed(t *testing.T) {
	c := &Config{RedirectURIs: []string{"https://hl/cb"}}
	if !c.RedirectAllowed("https://hl/cb") {
		t.Fatal("expected exact match")
	}
	if c.RedirectAllowed("https://hl/cb/extra") {
		t.Fatal("expected no prefix match")
	}
}

func validConfig() *Config {
	return &Config{
		Issuer:         "i",
		ClientID:       "c",
		RedirectURIs:   []string{"x"},
		AllowedDomains: []string{"d"},
		IdP: IdPConfig{
			Type: "oidc", Issuer: "https://idp", ClientID: "id", ClientSecret: "s",
			RedirectURL: "r", Scopes: []string{"openid"}, GroupsClaim: "groups",
		},
		Store: StoreConfig{Backend: "memory"},
	}
}

func TestValidate_RedisRequiresAddr(t *testing.T) {
	c := validConfig()
	c.Store.Backend = "redis"
	if err := c.validate(); err == nil {
		t.Fatal("expected error when redis selected without REDIS_ADDR")
	}
}

func TestValidate_GoogleRequiresDelegatedAdmin(t *testing.T) {
	c := validConfig()
	c.IdP.Type = "google"
	if err := c.validate(); err == nil {
		t.Fatal("expected error when google selected without GOOGLE_DELEGATED_ADMIN")
	}
}

func TestValidate_OK(t *testing.T) {
	if err := validConfig().validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

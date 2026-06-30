package broker

import (
	"encoding/json"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func TestSigner_SignAndVerify(t *testing.T) {
	s, generated, err := newSigner("https://broker.example", "")
	if err != nil {
		t.Fatal(err)
	}
	if !generated {
		t.Fatal("expected an ephemeral key to be generated for empty PEM")
	}

	tok, err := s.signIDToken(idClaims{
		Sub: "user-1", Aud: "dashboard", Nonce: "n",
		Email: "admin@kargus.io", EmailVerified: true, Groups: []string{"g/eng"},
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the signature against the published JWKS public key.
	jws, err := jose.ParseSigned(tok, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := jws.Verify(s.jwks().Keys[0])
	if err != nil {
		t.Fatalf("verify against jwks: %v", err)
	}

	var c idClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		t.Fatal(err)
	}
	if c.Iss != "https://broker.example" || c.Aud != "dashboard" || c.Sub != "user-1" {
		t.Fatalf("unexpected claims: %+v", c)
	}
	if len(c.Groups) != 1 || c.Groups[0] != "g/eng" {
		t.Fatalf("groups = %v", c.Groups)
	}
	if c.Exp <= c.Iat {
		t.Fatal("exp must be after iat")
	}
}

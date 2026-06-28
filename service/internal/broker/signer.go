package broker

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// signer mints and signs broker id_tokens (RS256) and exposes its public JWKS.
type signer struct {
	issuer string
	pub    jose.JSONWebKey
	jws    jose.Signer
}

// newSigner builds a signer from a PEM RSA key, or generates an ephemeral key
// when pemKey is empty (development only).
func newSigner(issuer, pemKey string) (*signer, bool, error) {
	var (
		priv      *rsa.PrivateKey
		generated bool
		err       error
	)
	if pemKey == "" {
		priv, err = rsa.GenerateKey(rand.Reader, 2048)
		generated = true
	} else {
		priv, err = parseRSAPrivateKey([]byte(pemKey))
	}
	if err != nil {
		return nil, false, err
	}

	pub := jose.JSONWebKey{Key: &priv.PublicKey, Algorithm: string(jose.RS256), Use: "sig"}
	tp, err := pub.Thumbprint(crypto.SHA256)
	if err != nil {
		return nil, false, err
	}
	pub.KeyID = base64.RawURLEncoding.EncodeToString(tp)

	jws, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: priv, KeyID: pub.KeyID}},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		return nil, false, err
	}
	return &signer{issuer: issuer, pub: pub, jws: jws}, generated, nil
}

type idClaims struct {
	Iss           string   `json:"iss"`
	Sub           string   `json:"sub"`
	Aud           string   `json:"aud"`
	Exp           int64    `json:"exp"`
	Iat           int64    `json:"iat"`
	Nonce         string   `json:"nonce,omitempty"`
	Email         string   `json:"email,omitempty"`
	EmailVerified bool     `json:"email_verified"`
	Name          string   `json:"name,omitempty"`
	Groups        []string `json:"groups,omitempty"`
}

// signIDToken returns a signed compact JWT for the subject.
func (s *signer) signIDToken(c idClaims, ttl time.Duration) (string, error) {
	now := time.Now()
	c.Iss = s.issuer
	c.Iat = now.Unix()
	c.Exp = now.Add(ttl).Unix()
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	obj, err := s.jws.Sign(payload)
	if err != nil {
		return "", err
	}
	return obj.CompactSerialize()
}

// jwks returns the public key set served at /jwks.
func (s *signer) jwks() jose.JSONWebKeySet {
	return jose.JSONWebKeySet{Keys: []jose.JSONWebKey{s.pub}}
}

func parseRSAPrivateKey(b []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("ID_TOKEN_SIGNING_KEY is not valid PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	rk, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("ID_TOKEN_SIGNING_KEY must be an RSA private key")
	}
	return rk, nil
}

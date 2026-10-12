// Package accesstoken signs access tokens: JWTs signed with Ed25519 (EdDSA), which the
// Edge verifies by itself with the public key (ADR 0002). It knows nothing about users
// beyond an ID and a role, so the account package can depend on it.
package accesstoken

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	Issuer   = "identity"
	Audience = "edge"
)

// The Edge can't revoke an access token, so this bounds how long a logged-out or
// stolen session keeps working.
const lifetime = 15 * time.Minute

// Claims are an access token's claims. Role is "rider" or "driver".
type Claims struct {
	jwt.RegisteredClaims
	Role string `json:"role"`
}

type Signer struct {
	key ed25519.PrivateKey
	kid string
	now func() time.Time
}

func New(key ed25519.PrivateKey, now func() time.Time) *Signer {
	return &Signer{key: key, kid: keyID(key.Public().(ed25519.PublicKey)), now: now}
}

// LoadKey reads the private key from a PKCS #8 PEM file, the format the Tiltfile's
// `openssl genpkey -algorithm ed25519` writes.
func LoadKey(path string) (ed25519.PrivateKey, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}
	key, err := jwt.ParseEdPrivateKeyFromPEM(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	// ParseEdPrivateKeyFromPEM has already rejected any other kind of key.
	return key.(ed25519.PrivateKey), nil
}

// Sign returns a signed access token for the user, and when it expires.
func (s *Signer) Sign(userID uuid.UUID, role string) (string, time.Time, error) {
	// A JWT holds whole seconds. Truncating first makes the expiry returned here, which
	// becomes the cookie's Max-Age, exactly the token's exp.
	now := s.now().Truncate(time.Second)
	expiresAt := now.Add(lifetime)

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		Role: role,
	})
	// The Edge picks the public key to verify with by this ID, so a new key can be
	// published next to the old one while tokens signed with the old one expire.
	token.Header["kid"] = s.kid

	signed, err := token.SignedString(s.key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

// keyID returns the key's RFC 7638 thumbprint: the SHA-256 of its public JWK's required
// members, which for Ed25519 are crv, kty and x (RFC 8037), in that order and without
// whitespace. Anyone holding the public key can compute it, and it never needs storing.
func keyID(pub ed25519.PublicKey) string {
	jwk := `{"crv":"Ed25519","kty":"OKP","x":"` + base64.RawURLEncoding.EncodeToString(pub) + `"}`
	sum := sha256.Sum256([]byte(jwk))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

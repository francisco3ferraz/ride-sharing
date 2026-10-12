package accesstoken

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Not on a whole second, so the tests see the truncation.
var signedAt = time.Date(2026, 10, 13, 9, 30, 0, 600_000_000, time.UTC)

func TestSign(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	userID := uuid.New()

	signed, expiresAt, err := New(key, func() time.Time { return signedAt }).Sign(userID, "rider")
	require.NoError(t, err)

	var claims Claims
	token, err := jwt.ParseWithClaims(signed, &claims, publicKeyOf(key), verifyAt(signedAt)...)
	require.NoError(t, err)

	assert.Equal(t, "EdDSA", token.Header["alg"])
	assert.Equal(t, keyID(key.Public().(ed25519.PublicKey)), token.Header["kid"])
	assert.Equal(t, userID.String(), claims.Subject)
	assert.Equal(t, "rider", claims.Role)
	assert.Equal(t, Issuer, claims.Issuer)
	assert.Equal(t, jwt.ClaimStrings{Audience}, claims.Audience)

	wholeSecond := signedAt.Truncate(time.Second)
	assert.Equal(t, wholeSecond.Unix(), claims.IssuedAt.Unix())
	assert.Equal(t, wholeSecond.Add(15*time.Minute), expiresAt)
	assert.Equal(t, expiresAt.Unix(), claims.ExpiresAt.Unix())
}

// Verified the way the Edge will verify it.
func TestSignedTokenVerification(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	_, otherKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	signed, expiresAt, err := New(key, func() time.Time { return signedAt }).Sign(uuid.New(), "driver")
	require.NoError(t, err)

	tests := []struct {
		name    string
		key     ed25519.PrivateKey // whose public key verifies
		at      time.Time
		wantErr error
	}{
		{name: "a second before it expires", key: key, at: expiresAt.Add(-time.Second)},
		{name: "when it expires", key: key, at: expiresAt, wantErr: jwt.ErrTokenExpired},
		{name: "with another key", key: otherKey, at: signedAt, wantErr: jwt.ErrTokenSignatureInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := jwt.ParseWithClaims(signed, &Claims{}, publicKeyOf(tt.key), verifyAt(tt.at)...)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// The example key and its thumbprint from RFC 8037, appendix A.
func TestKeyIDMatchesRFC8037(t *testing.T) {
	seed, err := base64.RawURLEncoding.DecodeString("nWGxne_9WmC6hEr0kuwsxERJxWl7MmkZcDusAxyuf2A")
	require.NoError(t, err)
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)

	assert.Equal(t, "11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo", base64.RawURLEncoding.EncodeToString(pub))
	assert.Equal(t, "kPrK_qmxVWaYVA9wwBF6Iuo3vVzz7TxHCTwXBygrS4k", keyID(pub))
}

func TestLoadKey(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{name: "an Ed25519 key", path: writePEM(t, key)},
		{name: "a missing file", path: filepath.Join(t.TempDir(), "missing.pem"), wantErr: fs.ErrNotExist},
		{name: "a file that isn't PEM", path: writeFile(t, []byte("not a key")), wantErr: jwt.ErrKeyMustBePEMEncoded},
		{name: "another kind of key", path: writePEM(t, ecdsaKey), wantErr: jwt.ErrNotEdPrivateKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadKey(tt.path)
			assert.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr == nil {
				assert.Equal(t, key, got)
			}
		})
	}
}

// verifyAt returns the options the Edge will verify tokens with, at the given time.
func verifyAt(at time.Time) []jwt.ParserOption {
	return []jwt.ParserOption{
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithIssuer(Issuer),
		jwt.WithAudience(Audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(func() time.Time { return at }),
	}
}

func publicKeyOf(key ed25519.PrivateKey) jwt.Keyfunc {
	return func(*jwt.Token) (any, error) { return key.Public(), nil }
}

// writePEM writes key as PKCS #8 PEM, as openssl genpkey does, and returns the path.
func writePEM(t *testing.T, key any) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return writeFile(t, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func writeFile(t *testing.T, contents []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "signing-key.pem")
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	return path
}

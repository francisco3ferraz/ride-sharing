// Package refreshtoken makes refresh tokens: opaque random values that the client holds
// and Identity stores only as SHA-256 hashes, so a database leak exposes no working
// token. A fast hash is enough because 256 random bits can't be guessed (ADR 0003).
package refreshtoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

func Generate() (token string, hash [32]byte) {
	b := make([]byte, 32)
	// Never fails: since Go 1.24 it crashes the program instead.
	_, _ = rand.Read(b)

	token = base64.RawURLEncoding.EncodeToString(b)
	return token, Hash(token)
}

// Hash hashes the token as the client sends it back, so a refresh needs no decoding.
// Changing what it hashes invalidates every stored refresh token.
func Hash(token string) [32]byte {
	return sha256.Sum256([]byte(token))
}

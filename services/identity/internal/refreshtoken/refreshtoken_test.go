package refreshtoken

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate(t *testing.T) {
	token, hash := Generate()

	b, err := base64.RawURLEncoding.Strict().DecodeString(token)
	require.NoError(t, err)
	assert.Len(t, b, 32)
	// A refresh finds the stored token by hashing what the client sends back.
	assert.Equal(t, Hash(token), hash)

	otherToken, otherHash := Generate()
	assert.NotEqual(t, token, otherToken)
	assert.NotEqual(t, hash, otherHash)
}

// Pins exactly what Hash hashes, which every stored refresh token depends on.
func TestHash(t *testing.T) {
	hash := Hash("q3Lw0mXnVb9cT5yEoR2dJ8hKfZ1aUs7GiP4NtB6vYxC")
	assert.Equal(t, "12c55b68b6004127535645b67015d6856f6ed140e0adcf0908a28b693b5ab61c", hex.EncodeToString(hash[:]))
}

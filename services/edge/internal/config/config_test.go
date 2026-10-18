package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	// Setenv restores the variables after the test, so it's safe to unset them here.
	for _, key := range []string{"LISTEN_ADDR", "PRICING_ADDR", "IDENTITY_ADDR", "IDENTITY_JWKS_URL", "VALKEY_ADDR", "VALKEY_USERNAME", "VALKEY_PASSWORD"} {
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, ":8080", cfg.ListenAddr)
	assert.Equal(t, "localhost:50051", cfg.PricingAddr)
	assert.Equal(t, "localhost:50052", cfg.IdentityAddr)
	assert.Equal(t, "http://localhost:8081/.well-known/jwks.json", cfg.IdentityJWKSURL)
	assert.Equal(t, "localhost:6379", cfg.ValkeyAddr)
	assert.Equal(t, "edge", cfg.ValkeyUsername)
	assert.Empty(t, cfg.ValkeyPassword)
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9000")
	t.Setenv("PRICING_ADDR", "pricing:50051")
	t.Setenv("IDENTITY_ADDR", "identity:50052")
	t.Setenv("IDENTITY_JWKS_URL", "http://identity:8080/.well-known/jwks.json")
	t.Setenv("VALKEY_ADDR", "valkey:6379")
	t.Setenv("VALKEY_USERNAME", "someone")
	t.Setenv("VALKEY_PASSWORD", "secret")

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, ":9000", cfg.ListenAddr)
	assert.Equal(t, "pricing:50051", cfg.PricingAddr)
	assert.Equal(t, "identity:50052", cfg.IdentityAddr)
	assert.Equal(t, "http://identity:8080/.well-known/jwks.json", cfg.IdentityJWKSURL)
	assert.Equal(t, "valkey:6379", cfg.ValkeyAddr)
	assert.Equal(t, "someone", cfg.ValkeyUsername)
	assert.Equal(t, "secret", cfg.ValkeyPassword)
}

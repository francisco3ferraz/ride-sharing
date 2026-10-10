package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	// Setenv restores the variables after the test, so it's safe to unset them here.
	for _, key := range []string{"LISTEN_ADDR", "PRICING_ADDR"} {
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, ":8080", cfg.ListenAddr)
	assert.Equal(t, "localhost:50051", cfg.PricingAddr)
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9000")
	t.Setenv("PRICING_ADDR", "pricing:50051")

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, ":9000", cfg.ListenAddr)
	assert.Equal(t, "pricing:50051", cfg.PricingAddr)
}

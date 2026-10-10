package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	// Setenv restores the variable after the test, so it's safe to unset it here.
	t.Setenv("LISTEN_ADDR", "")
	require.NoError(t, os.Unsetenv("LISTEN_ADDR"))

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, ":50051", cfg.ListenAddr)
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9000")

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, ":9000", cfg.ListenAddr)
}

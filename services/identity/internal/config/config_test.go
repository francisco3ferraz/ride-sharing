package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	// Setenv restores each variable after the test, so it's safe to unset it here.
	for _, name := range []string{"LISTEN_ADDR", "JWT_KEY_PATH"} {
		t.Setenv(name, "")
		require.NoError(t, os.Unsetenv(name))
	}

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, ":50052", cfg.ListenAddr)
	// Where the Deployment mounts the jwt-signing-key Secret.
	assert.Equal(t, "/etc/jwt-signing-key/signing-key.pem", cfg.JWTKeyPath)
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9000")
	t.Setenv("JWT_KEY_PATH", "/tmp/signing-key.pem")

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, ":9000", cfg.ListenAddr)
	assert.Equal(t, "/tmp/signing-key.pem", cfg.JWTKeyPath)
}

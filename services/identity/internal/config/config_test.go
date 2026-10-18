package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	// Setenv restores each variable after the test, so it's safe to unset it here.
	for _, name := range []string{"LISTEN_ADDR", "HTTP_LISTEN_ADDR", "JWT_KEY_PATH", "SMTP_HOST", "SMTP_PORT", "MAIL_FROM", "WEB_URL"} {
		t.Setenv(name, "")
		require.NoError(t, os.Unsetenv(name))
	}

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, ":50052", cfg.ListenAddr)
	assert.Equal(t, ":8080", cfg.HTTPListenAddr)
	// Where the Deployment mounts the jwt-signing-key Secret.
	assert.Equal(t, "/etc/jwt-signing-key/signing-key.pem", cfg.JWTKeyPath)
	assert.Equal(t, "mailpit-smtp.mailpit.svc", cfg.SMTPHost)
	assert.Equal(t, 25, cfg.SMTPPort)
	assert.Equal(t, "Ride-sharing <no-reply@ride-sharing.localhost>", cfg.MailFrom)
	assert.Equal(t, "https://ride-sharing.localhost", cfg.WebURL)
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9000")
	t.Setenv("HTTP_LISTEN_ADDR", ":9001")
	t.Setenv("JWT_KEY_PATH", "/tmp/signing-key.pem")
	t.Setenv("SMTP_HOST", "localhost")
	t.Setenv("SMTP_PORT", "1025")
	t.Setenv("MAIL_FROM", "no-reply@example.pt")
	t.Setenv("WEB_URL", "http://localhost:5173")

	cfg, err := Load()

	require.NoError(t, err)
	assert.Equal(t, ":9000", cfg.ListenAddr)
	assert.Equal(t, ":9001", cfg.HTTPListenAddr)
	assert.Equal(t, "/tmp/signing-key.pem", cfg.JWTKeyPath)
	assert.Equal(t, "localhost", cfg.SMTPHost)
	assert.Equal(t, 1025, cfg.SMTPPort)
	assert.Equal(t, "no-reply@example.pt", cfg.MailFrom)
	assert.Equal(t, "http://localhost:5173", cfg.WebURL)
}

package email

import (
	"bytes"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasswordReset(t *testing.T) {
	sender, err := New("mailpit-smtp.mailpit.svc", 25, "Ride-sharing <no-reply@ride-sharing.localhost>", "https://ride-sharing.localhost")
	require.NoError(t, err)

	msg, err := sender.passwordReset("ana@example.pt", "the-token")
	require.NoError(t, err)

	var raw bytes.Buffer
	_, err = msg.WriteTo(&raw)
	require.NoError(t, err)
	parsed, err := mail.ReadMessage(&raw)
	require.NoError(t, err)
	assert.Equal(t, "<ana@example.pt>", parsed.Header.Get("To"))
	assert.Equal(t, `"Ride-sharing" <no-reply@ride-sharing.localhost>`, parsed.Header.Get("From"))
	assert.Equal(t, "Reset your password", parsed.Header.Get("Subject"))

	require.Equal(t, "quoted-printable", parsed.Header.Get("Content-Transfer-Encoding"))
	body, err := io.ReadAll(quotedprintable.NewReader(parsed.Body))
	require.NoError(t, err)
	// In the fragment, never the query string. Email lines end in CRLF.
	assert.Contains(t, string(body), "\r\nhttps://ride-sharing.localhost/reset-password#token=the-token\r\n")
}

func TestPasswordResetRejectsBadAddresses(t *testing.T) {
	sender, err := New("mailpit-smtp.mailpit.svc", 25, "Ride-sharing <no-reply@ride-sharing.localhost>", "https://ride-sharing.localhost")
	require.NoError(t, err)

	_, err = sender.passwordReset("not an address", "the-token")
	assert.Error(t, err)
}

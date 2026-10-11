package password

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashRule(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "14 characters", password: strings.Repeat("a", 14), wantErr: true},
		{name: "15 characters", password: strings.Repeat("a", 15)},
		{name: "128 characters", password: strings.Repeat("a", 128)},
		{name: "129 characters", password: strings.Repeat("a", 129), wantErr: true},
		{name: "counts characters, not bytes", password: strings.Repeat("🔑", 15)},
	}

	h := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.Hash(t.Context(), tt.password)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrInvalid)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	h := New()
	encoded, err := h.Hash(t.Context(), "a minha palavra-passe é longa")
	require.NoError(t, err)

	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{name: "same password", password: "a minha palavra-passe é longa"},
		{name: "same password, é as e plus an accent", password: "a minha palavra-passe é longa"},
		{name: "different password", password: "a minha palavra-passe é curta", wantErr: ErrMismatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorIs(t, h.Verify(t.Context(), tt.password, encoded), tt.wantErr)
		})
	}
}

func TestHashSaltsEveryHash(t *testing.T) {
	h := New()
	first, err := h.Hash(t.Context(), "the same password twice")
	require.NoError(t, err)
	second, err := h.Hash(t.Context(), "the same password twice")
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

func TestVerifyRejectsOtherHashes(t *testing.T) {
	h := New()
	for _, encoded := range []string{
		"",
		strings.Replace(dummyHash, "argon2id", "argon2i", 1),
		strings.Replace(dummyHash, "t=2", "t=3", 1),
		prefix + "not base64!$AAAA",
	} {
		err := h.Verify(t.Context(), "a long enough password", encoded)
		require.Error(t, err, encoded)
		assert.NotErrorIs(t, err, ErrMismatch, encoded)
	}
}

func TestVerifyDummy(t *testing.T) {
	assert.NoError(t, New().VerifyDummy(t.Context(), "any password at all"))
}

func TestHashingWaitsForASlot(t *testing.T) {
	h := New()
	require.NoError(t, h.slots.Acquire(t.Context(), maxConcurrent))
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := h.Hash(ctx, "a long enough password")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorIs(t, h.Verify(ctx, "a long enough password", dummyHash), context.DeadlineExceeded)
}

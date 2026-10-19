// Package password checks, hashes and verifies user passwords. It knows nothing about
// gRPC or the database.
package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/sync/semaphore"
	"golang.org/x/text/unicode/norm"
)

// NIST SP 800-63B-4: a length range, and no composition rules.
const (
	minRunes = 15
	maxRunes = 128
)

const (
	memoryKiB = 19 * 1024
	passes    = 2
	threads   = 1
	saltBytes = 16
	keyBytes  = 32
	// Identity's memory limit and GOMEMLIMIT assume at most this many hashes at once.
	maxConcurrent = 4
	// Past this many waiting for a slot, a hash is turned away at once. A full queue
	// waits four hashes' time, about 200 ms in a burst on a laptop, which must stay well
	// inside the Edge's timeout for Identity calls: a queue holding that timeout's worth
	// spends every slot on a login about to time out.
	maxQueued = 16
)

var b64 = base64.RawStdEncoding

// Verify accepts only hashes with these parameters, so a stored hash can't choose its
// own cost. Raising the cost later means accepting the old prefix as well.
var prefix = fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$", argon2.Version, memoryKiB, passes, threads)

// Verifying against it costs as much as against a real hash, and its all-zero key
// matches no password.
var dummyHash = prefix + b64.EncodeToString(make([]byte, saltBytes)) + "$" + b64.EncodeToString(make([]byte, keyBytes))

var (
	// ErrInvalid is wrapped by Hash's errors for a password that breaks the rule. The
	// full message is written for the user.
	ErrInvalid  = errors.New("invalid password")
	ErrMismatch = errors.New("password does not match")
	// ErrBusy means too many hashes were running or waiting to take another, so nothing
	// was hashed.
	ErrBusy = errors.New("too many passwords are being hashed")
)

type Hasher struct {
	slots *semaphore.Weighted
	// Hashes running or waiting for a slot.
	admitted atomic.Int64
}

func New() *Hasher {
	return &Hasher{slots: semaphore.NewWeighted(maxConcurrent)}
}

func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	switch n := utf8.RuneCountInString(password); {
	case n < minRunes:
		return "", fmt.Errorf("%w: must be at least %d characters", ErrInvalid, minRunes)
	case n > maxRunes:
		return "", fmt.Errorf("%w: must be at most %d characters", ErrInvalid, maxRunes)
	}

	salt := make([]byte, saltBytes)
	rand.Read(salt)
	return h.hash(ctx, password, salt)
}

// Verify returns ErrMismatch if password doesn't match encoded, and ErrBusy if it was
// turned away. Any other error means encoded wasn't made by Hash, or ctx ended while
// waiting for a slot.
func (h *Hasher) Verify(ctx context.Context, password, encoded string) error {
	rest, ok := strings.CutPrefix(encoded, prefix)
	if !ok {
		return errors.New("parse hash: unexpected algorithm or parameters")
	}
	encodedSalt, _, _ := strings.Cut(rest, "$")
	salt, err := b64.DecodeString(encodedSalt)
	if err != nil {
		return fmt.Errorf("decode salt: %w", err)
	}

	rehashed, err := h.hash(ctx, password, salt)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(rehashed), []byte(encoded)) != 1 {
		return ErrMismatch
	}
	return nil
}

// VerifyDummy does Verify's work when a login's email doesn't exist, so it takes as
// long as a wrong password, and is turned away the same way. It returns an error only
// if it's turned away or ctx ends while waiting.
func (h *Hasher) VerifyDummy(ctx context.Context, password string) error {
	err := h.Verify(ctx, password, dummyHash)
	if errors.Is(err, ErrMismatch) {
		return nil
	}
	return err
}

func (h *Hasher) hash(ctx context.Context, password string, salt []byte) (string, error) {
	// Counted before waiting, so running and waiting hashes share one limit, and
	// uncounted however this returns.
	if h.admitted.Add(1) > maxConcurrent+maxQueued {
		h.admitted.Add(-1)
		return "", ErrBusy
	}
	defer h.admitted.Add(-1)

	if err := h.slots.Acquire(ctx, 1); err != nil {
		return "", fmt.Errorf("wait for hashing slot: %w", err)
	}
	defer h.slots.Release(1)

	// NFKC makes the same password typed on different devices the same bytes. Changing
	// it would lock out every user whose password it changes.
	normalized := norm.NFKC.String(password)
	key := argon2.IDKey([]byte(normalized), salt, passes, memoryKiB, threads, keyBytes)
	return prefix + b64.EncodeToString(salt) + "$" + b64.EncodeToString(key), nil
}

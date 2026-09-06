// File: internal/shared/crypt/password.go

package crypt

import (
	"golang.org/x/crypto/bcrypt"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// HashPassword hashes a plaintext password with the configured work factor.
//
// The cost comes from BCRYPT_COST rather than bcrypt.DefaultCost, which is the
// whole point of making it configurable: DefaultCost is 10 and has not moved in
// a decade, while the hardware an attacker rents has.
func (s *Service) HashPassword(password string) (string, error) {
	var hashed []byte
	var err error
	hashed, err = bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost)
	if err != nil {
		// bcrypt fails on a cost outside 4..31 and on a password over 72
		// bytes. Both are our fault — the cost is validated at startup, and the
		// length is checked before we get here.
		return "", errors.NewInternalError("could not hash the password", err)
	}
	return string(hashed), nil
}

// ComparePassword checks a plaintext password against a stored hash.
//
// It returns an *AppError classified as INVALID_CREDENTIALS for a mismatch, so
// a caller that forgets to translate still cannot leak "wrong password" as a
// 500. Every other failure — a corrupt or truncated hash — is INTERNAL, because
// it means the stored value is not a bcrypt hash at all.
//
// bcrypt's own comparison is constant-time with respect to the hash contents.
func (s *Service) ComparePassword(hash string, password string) error {
	var err error = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err == nil {
		return nil
	}

	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return errors.NewInvalidCredentialsError("invalid credentials", err)
	}
	return errors.NewInternalError("stored password hash is unusable", err)
}

// NeedsRehash reports whether a stored hash was produced with a lower work
// factor than the one now configured.
//
// bcrypt writes its cost into the hash string, so raising BCRYPT_COST can be
// rolled out without a migration: on the next successful login the caller
// re-hashes the password it has just verified and stores the result. Users
// migrate as they appear, and one that never logs in again never needs to.
//
// A hash that cannot be parsed reports false. It is not a *stale* hash, it is a
// broken one, and re-hashing on the strength of an unreadable cost would be
// guessing.
func (s *Service) NeedsRehash(hash string) bool {
	var cost int
	var err error
	cost, err = bcrypt.Cost([]byte(hash))
	if err != nil {
		return false
	}
	return cost < s.bcryptCost
}

// DummyCompare burns roughly the time a real comparison would take.
//
// Login must not answer faster for an address that has no account than for one
// that does: the difference is measurable over a few hundred requests and turns
// the endpoint into a user-enumeration oracle. When there is no user to check,
// the caller compares against this throwaway hash instead and discards the
// result.
//
// **The hash is built at the configured cost**, not at a fixed one. bcrypt's
// running time is set by the cost baked into the hash, so a constant at cost 12
// would take the wrong amount of time on any deployment that had raised
// BCRYPT_COST — leaving exactly the timing difference this exists to remove.
func (s *Service) DummyCompare(password string) {
	_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
}

// newDummyHash hashes an unguessable value at cost, once, at startup.
//
// A failure here cannot be returned — the caller is a constructor with no error
// to give — but it must not leave an unusable hash either, because
// CompareHashAndPassword on a malformed one returns immediately and does none
// of the work. Falling back to a fixed hash keeps the masking, just at a cost
// that may not match; that beats no masking at all.
func newDummyHash(cost int) []byte {
	var nonce string
	var err error
	nonce, err = RandomString(32)
	if err != nil {
		nonce = "the-entropy-source-failed-at-startup"
	}

	var hashed []byte
	hashed, err = bcrypt.GenerateFromPassword([]byte(nonce), cost)
	if err != nil {
		return []byte(fallbackDummyHash)
	}
	return hashed
}

// fallbackDummyHash is a valid bcrypt hash at cost 12, used only if hashing at
// the configured cost fails. It is not a credential: the value it hashes was
// random and discarded, and nothing ever compares equal to it on purpose.
const fallbackDummyHash = "$2a$12$C6UzMDM.H6dfI/f/IKcEe.eS9wZmvXWx0hSs.i1sD8dRvfBhOvIWi" //nolint:gosec // G101: a throwaway hash, not a credential

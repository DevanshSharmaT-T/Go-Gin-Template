// File: internal/shared/crypt/password_test.go

package crypt

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
)

func TestHashPassword_VerifiesAndSalts(t *testing.T) {
	s := testService(t)

	first, err := s.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	second, err := s.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	// bcrypt salts automatically, so the same password must never produce the
	// same hash — otherwise the column is a rainbow-table lookup.
	if first == second {
		t.Fatal("two hashes of one password are identical; the salt is not being applied")
	}
	if strings.Contains(first, "correct horse") {
		t.Fatal("the hash contains the password")
	}

	if err := s.ComparePassword(first, "correct horse battery staple"); err != nil {
		t.Fatalf("the password did not verify against its own hash: %v", err)
	}
	if err := s.ComparePassword(second, "correct horse battery staple"); err != nil {
		t.Fatalf("the password did not verify against its own hash: %v", err)
	}
}

// A wrong password must classify as INVALID_CREDENTIALS, so a caller that
// forgets to translate still cannot turn it into a 500.
func TestComparePassword_WrongPasswordIsUnauthorized(t *testing.T) {
	s := testService(t)

	hash, err := s.HashPassword("the real password")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	err = s.ComparePassword(hash, "not the real password")
	if err == nil {
		t.Fatal("a wrong password verified")
	}
	assertStatus(t, err, http.StatusUnauthorized)
}

// A corrupt stored hash is our problem, not the caller's, and must not be
// reported as a failed login.
func TestComparePassword_CorruptHashIsInternal(t *testing.T) {
	s := testService(t)

	err := s.ComparePassword("this is not a bcrypt hash", "anything")
	if err == nil {
		t.Fatal("a corrupt hash verified")
	}
	assertStatus(t, err, http.StatusInternalServerError)
}

// The promise in docs/LIBRARIES.md: raising BCRYPT_COST re-hashes users on
// their next login, with no migration.
func TestNeedsRehash_DetectsAnOutdatedCost(t *testing.T) {
	low := &Service{bcryptCost: 4, tokenSecret: []byte("k")}
	high := &Service{bcryptCost: 6, tokenSecret: []byte("k")}

	hash, err := low.HashPassword("a password")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	if low.NeedsRehash(hash) {
		t.Fatal("a hash at the configured cost was reported as stale")
	}
	if !high.NeedsRehash(hash) {
		t.Fatal("a hash below the configured cost was not reported as stale")
	}

	// Re-hashing at the higher cost settles it.
	upgraded, err := high.HashPassword("a password")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	if high.NeedsRehash(upgraded) {
		t.Fatal("a freshly written hash was reported as stale")
	}
}

// A hash that cannot be parsed is broken, not stale: re-hashing on the strength
// of an unreadable cost would be guessing.
func TestNeedsRehash_IgnoresAnUnparseableHash(t *testing.T) {
	s := testService(t)
	if s.NeedsRehash("not a hash at all") {
		t.Fatal("an unparseable hash was reported as stale")
	}
}

// The dummy hash has to be a real bcrypt hash, or DummyCompare returns early
// and does none of the work it exists to spend — and it has to carry the
// *configured* cost, or the time it burns does not match a real comparison.
func TestDummyCompare_UsesAParseableHashAtTheConfiguredCost(t *testing.T) {
	for _, cost := range []int{4, 6} {
		t.Run(strconv.Itoa(cost), func(t *testing.T) {
			s := NewService(&config.Config{Auth: config.Auth{
				JWTSecret:  "a-test-jwt-signing-key-of-sufficient-length",
				BcryptCost: cost,
			}})

			got, err := bcrypt.Cost(s.dummyHash)
			if err != nil {
				t.Fatalf("the dummy hash does not parse, so comparing costs nothing: %v", err)
			}
			if got != cost {
				t.Fatalf("dummy hash cost: want %d, got %d", cost, got)
			}

			// It must never match, whatever is thrown at it.
			s.DummyCompare("")
			s.DummyCompare("password")
			if err := bcrypt.CompareHashAndPassword(s.dummyHash, []byte("password")); err == nil {
				t.Fatal("the dummy hash matched a guessable password")
			}
		})
	}
}

// The fallback is only reached if hashing at the configured cost fails, but it
// still has to be a hash — an unparseable one would make DummyCompare return
// immediately and reinstate the timing difference.
func TestFallbackDummyHash_Parses(t *testing.T) {
	if _, err := bcrypt.Cost([]byte(fallbackDummyHash)); err != nil {
		t.Fatalf("the fallback dummy hash does not parse: %v", err)
	}
}

// bcrypt refuses anything over 72 bytes; the rules layer rejects it first, but
// the hasher must not be the thing that silently truncates.
func TestHashPassword_RejectsOverlongInput(t *testing.T) {
	s := testService(t)

	if _, err := s.HashPassword(strings.Repeat("a", 100)); err == nil {
		t.Fatal("a 100-byte password was hashed; bcrypt would have truncated it")
	}
}

func TestRandomString_IsRandomAndURLSafe(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		value, err := RandomString(16)
		if err != nil {
			t.Fatalf("generating: %v", err)
		}
		if seen[value] {
			t.Fatal("RandomString repeated a value")
		}
		seen[value] = true

		if strings.ContainsAny(value, "+/=") {
			t.Fatalf("value is not URL-safe: %q", value)
		}
	}

	if _, err := RandomString(0); err == nil {
		t.Fatal("a zero length was accepted")
	}
}

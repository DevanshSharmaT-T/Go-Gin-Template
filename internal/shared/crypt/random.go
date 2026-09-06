// File: internal/shared/crypt/random.go

package crypt

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// nonceBytes is the entropy in a token's nonce. 128 bits is the usual floor for
// a value that must not be guessable and is not a key.
const nonceBytes = 16

// RandomString returns n cryptographically random bytes, base64url-encoded
// without padding so the result is safe in a URL path or query string.
//
// crypto/rand.Read is documented never to return a short read without an error,
// so a nil error means the buffer is fully populated.
func RandomString(n int) (string, error) {
	if n <= 0 {
		return "", errors.NewInternalError("random length must be positive", nil)
	}

	var buf []byte = make([]byte, n)
	var err error
	_, err = rand.Read(buf)
	if err != nil {
		// The system entropy source failed. There is no safe fallback: the
		// alternative is a predictable token, so this has to be an error the
		// caller propagates rather than something we paper over.
		return "", errors.NewInternalError("could not read from the system random source", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// tokenSecretInfo labels the HKDF expansion that derives the token-signing key
// from JWT_SECRET.
//
// The label is what makes the derived key independent of the JWT signing key:
// HKDF with the same input and a different info string produces an unrelated
// output, so recovering one key tells an attacker nothing about the other. Do
// not change it — every outstanding verification and reset token is signed with
// the key it produces, and they would all stop verifying at once.
const tokenSecretInfo = "go-gin-template/verification-token/v1" //nolint:gosec // G101: an HKDF domain-separation label, not a secret

// tokenSecretLength is 32 bytes, matching HMAC-SHA256's security level.
const tokenSecretLength = 32

// deriveTokenSecret expands JWT_SECRET into a separate key for verification
// tokens.
//
// Reusing one secret for two purposes is the mistake this avoids. A JWT signing
// key and a token-signing key have different lifetimes and different exposure,
// and sharing material means rotating one forces rotating both. Deriving costs
// one HMAC and keeps them independent, while still letting an operator
// configure a single secret — which is what most will do.
//
// No salt is passed, which HKDF permits: the input is already a high-entropy
// secret rather than a password, so the extract step has nothing to strengthen.
func deriveTokenSecret(jwtSecret string) []byte {
	var key []byte
	var err error
	key, err = hkdf.Key(sha256.New, []byte(jwtSecret), nil, tokenSecretInfo, tokenSecretLength)
	if err != nil {
		// HKDF over a 32-byte output is one HMAC invocation on an in-memory
		// buffer. It has no I/O and no way to fail that is not a programming
		// error here, and continuing with a zero key would silently make every
		// token forgeable.
		panic("crypt: deriving the token secret failed: " + err.Error())
	}
	return key
}

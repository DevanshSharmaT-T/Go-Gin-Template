// File: internal/shared/crypt/crypt.go

// Package crypt holds the three cryptographic primitives the application needs:
// password hashing, HMAC-signed single-use tokens, and secure random values.
//
// It exists as one package so that every use of cryptography in the codebase is
// in one place to audit, and so that no module reaches for crypto/rand or
// bcrypt directly and gets a detail wrong.
//
// Three rules hold throughout:
//
//   - Randomness always comes from crypto/rand. math/rand is seeded, its output
//     is predictable from any two values, and it has no place anywhere near a
//     token.
//   - Comparisons of secrets use hmac.Equal or bcrypt's own comparison, both of
//     which are constant-time. A plain == on two signatures leaks their common
//     prefix through timing.
//   - Nothing here formats an error with the value it failed on. A cryptography
//     package is the last place that should be helpfully echoing a token back
//     into a log line.
package crypt

import (
	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
)

// Module provides the crypt service.
var Module = fx.Module("crypt",
	fx.Provide(NewService),
)

// Service carries the settings the primitives need — the bcrypt work factor and
// the token-signing key — so that no call site has to remember to pass them.
//
// It is a struct rather than a set of package functions because both of those
// are configuration, and configuration reaching a package-level function means
// either a global or an argument every caller can get wrong.
type Service struct {
	bcryptCost  int
	tokenSecret []byte

	// dummyHash is a bcrypt hash at bcryptCost of a value nobody can supply.
	// DummyCompare burns time against it on the unknown-user path; see there
	// for why it has to exist and why it is built at the configured cost.
	dummyHash []byte
}

// NewService builds the service from configuration.
//
// The token-signing key is derived from JWT_SECRET when TOKEN_SECRET is unset,
// using HKDF with a distinct info label — see deriveTokenSecret. That keeps the
// two key usages from sharing material even in the default configuration, where
// an operator has set exactly one secret.
func NewService(cfg *config.Config) *Service {
	var secret []byte
	if cfg.Auth.TokenSecretIsDerived() {
		secret = deriveTokenSecret(cfg.Auth.JWTSecret)
	} else {
		secret = []byte(cfg.Auth.TokenSecret)
	}

	return &Service{
		bcryptCost:  cfg.Auth.BcryptCost,
		tokenSecret: secret,
		dummyHash:   newDummyHash(cfg.Auth.BcryptCost),
	}
}

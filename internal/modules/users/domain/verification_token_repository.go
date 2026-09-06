// File: internal/modules/users/domain/verification_token_repository.go

package domain

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/crypt"
)

// VerificationTokenRepository is the persistence port for issued tokens.
type VerificationTokenRepository interface {
	Create(ctx context.Context, token *VerificationToken) error

	// FindByFingerprint returns (nil, nil) when the token was never issued —
	// which, for a forged or already-purged token, is the normal answer.
	FindByFingerprint(ctx context.Context, fingerprint string) (*VerificationToken, error)

	// Consume marks a token redeemed. It must be conditional on the token still
	// being unconsumed and report whether it won, so that two requests
	// presenting the same link concurrently cannot both succeed. Checking
	// Usable and then updating is a race; the database has to arbitrate.
	Consume(ctx context.Context, id uuid.UUID, at time.Time) (bool, error)

	// InvalidateOutstanding consumes every unredeemed token a user holds for a
	// purpose. Issuing a new reset link must retire the previous one, or a
	// stolen older link stays live for the rest of its TTL.
	InvalidateOutstanding(ctx context.Context, userID uuid.UUID, purpose crypt.Purpose, at time.Time) error

	// DeleteExpired removes tokens that are past their expiry, for a
	// housekeeping job. Nothing calls it yet; it exists so the table has a
	// documented way not to grow without bound.
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

// File: internal/modules/users/domain/verification_token_model.go

package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/crypt"
)

// VerificationToken records an issued email-verification or password-reset
// token so that it can be used exactly once.
//
// **Why a row at all, when the token is already signed?** The HMAC proves the
// token was minted by this service and has not been altered, and it does that
// without a database round trip. What a signature cannot prove is that the
// token has not already been used — a stateless token has no memory. Without
// this table a reset link keeps working for its whole TTL, so anyone who later
// reads it out of a mailbox, a proxy log or a browser history can reuse it.
//
// **Only the fingerprint is stored, never the token.** The column holds a
// SHA-256 of the token, so a leaked backup of this table cannot be turned into
// working links. Verification hashes the presented token and looks for a match.
type VerificationToken struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index"`

	// Fingerprint is crypt.Fingerprint of the issued token. Unique, so the same
	// token can never be recorded twice.
	Fingerprint string `gorm:"uniqueIndex;not null;type:varchar(64)"`

	Purpose   crypt.Purpose `gorm:"not null;type:varchar(32);index"`
	ExpiresAt time.Time     `gorm:"not null;index"`

	// ConsumedAt is nil until the token is redeemed. Redeemed tokens are kept
	// rather than deleted: "this link was already used" is a question worth
	// being able to answer during an incident.
	ConsumedAt *time.Time

	CreatedAt time.Time `gorm:"autoCreateTime"`
}

// TableName pins the table name.
func (VerificationToken) TableName() string {
	return "verification_tokens"
}

// Usable reports whether the token may still be redeemed at now.
func (t *VerificationToken) Usable(now time.Time) bool {
	return t.ConsumedAt == nil && now.Before(t.ExpiresAt)
}

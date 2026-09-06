// File: internal/modules/auth/service/auth_dto.go

package service

import (
	"time"

	"github.com/google/uuid"
)

// RegisterRequestDTO is the public registration payload.
//
// **There is no role_id field, and that is a security control rather than an
// oversight.** A role taken from the registration body is privilege escalation
// by JSON field: anyone who can read the API docs can self-provision an
// administrator. The role is assigned server-side from
// domain.DefaultRoleID, and changing it goes through a permission-gated
// endpoint. Adding a field here re-opens the hole, so do not.
//
// Status is absent for the same reason: registration produces an unverified,
// inactive account, and only the email-verification flow activates it.
type RegisterRequestDTO struct {
	Username  string `json:"username" binding:"required"`
	Email     string `json:"email" binding:"required"`
	Password  string `json:"password" binding:"required"`
	FirstName string `json:"first_name" binding:"required"`
	LastName  string `json:"last_name" binding:"required"`
}

// LoginRequestDTO authenticates with either a username or an email address.
//
// One field takes both because making the client choose gains nothing and
// telling them which one was wrong would be an enumeration oracle either way.
type LoginRequestDTO struct {
	Identifier string `json:"identifier" binding:"required"`
	Password   string `json:"password" binding:"required"`
}

// AuthResponseDTO is what a successful login returns.
//
// ExpiresIn is seconds, matching OAuth 2.0's convention, so a client can
// schedule a refresh without parsing the token it is not supposed to read.
type AuthResponseDTO struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresIn   int64     `json:"expires_in"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// VerifyEmailRequestDTO confirms an address.
type VerifyEmailRequestDTO struct {
	Token string `json:"token" binding:"required"`
}

// ForgotPasswordRequestDTO starts a password reset.
type ForgotPasswordRequestDTO struct {
	Identifier string `json:"identifier" binding:"required"`
}

// ResetPasswordRequestDTO completes one.
//
// The token is the only authorisation. It never carries a password hash — a
// hash in a URL leaks through referrer headers, browser history, proxy logs and
// anything that indexes a mailbox, and unlike a token it cannot be revoked
// because it *is* the credential.
type ResetPasswordRequestDTO struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// RegisteredDTO acknowledges a registration.
//
// It carries no access token. The account is inactive until the address is
// verified, and returning a token here would both contradict that and hand one
// out to whoever registered an address they may not own.
type RegisteredDTO struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
	Email    string    `json:"email"`
	Message  string    `json:"message"`
}

// MessageResponseDTO is the deliberately uninformative acknowledgement returned
// by flows that must not reveal whether an account exists.
type MessageResponseDTO struct {
	Message string `json:"message"`
}

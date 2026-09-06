// File: internal/modules/users/domain/user_model.go

package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// UserStatus is the account's lifecycle state. It is the first thing login
// checks, before it looks at a password.
type UserStatus string

const (
	// UserStatusInactive is where every registration starts. The account
	// exists and cannot be used until the email address is confirmed, which is
	// what stops anyone from registering an address they do not control and
	// what makes password reset meaningful.
	UserStatusInactive UserStatus = "inactive"

	// UserStatusActive is a confirmed, usable account.
	UserStatusActive UserStatus = "active"

	// UserStatusSuspended is an account an administrator has disabled. It is
	// distinct from deletion: the row and its history stay.
	UserStatusSuspended UserStatus = "suspended"
)

// Valid reports whether s is one of the declared states. It exists so a status
// arriving from a request body cannot write an arbitrary string into the
// column and quietly create an account that is neither active nor blocked.
func (s UserStatus) Valid() bool {
	switch s {
	case UserStatusInactive, UserStatusActive, UserStatusSuspended:
		return true
	}
	return false
}

// String makes UserStatus printable.
func (s UserStatus) String() string { return string(s) }

// DefaultRoleID is the role every registration is assigned.
//
// It is a constant here, and not something a request can influence, because the
// alternative is the vulnerability described in docs/SECURITY.md: a role taken
// from the registration body is privilege escalation by JSON field. Role
// changes go through a permission-gated endpoint instead.
//
// The value matches the seeded USER role in the roles module.
const DefaultRoleID int64 = 4

// User is an account.
//
// PasswordHash carries `json:"-"` as a second line of defence. The first is
// that services return DTOs rather than entities, so the struct should never
// reach a JSON encoder at all — but a tag costs nothing and covers the day
// somebody marshals a User for a log line.
type User struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Username     string    `gorm:"uniqueIndex;not null;type:varchar(50)"`
	Email        string    `gorm:"uniqueIndex;not null;type:varchar(255)"`
	PasswordHash string    `gorm:"not null;type:varchar(255)" json:"-"`

	FirstName string `gorm:"not null;type:varchar(100)"`
	LastName  string `gorm:"not null;type:varchar(100)"`

	RoleID int64      `gorm:"not null;index"`
	Status UserStatus `gorm:"not null;type:varchar(20);default:'inactive'"`

	EmailVerifiedAt *time.Time
	LastLoginAt     *time.Time

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// TableName pins the table name so renaming the Go type cannot orphan the data.
func (User) TableName() string {
	return "users"
}

// FullName joins the two name fields for display and for mail templates.
func (u *User) FullName() string {
	return strings.TrimSpace(u.FirstName + " " + u.LastName)
}

// IsActive reports whether the account may authenticate.
func (u *User) IsActive() bool {
	return u.Status == UserStatusActive
}

// IsSuspended reports whether an administrator has disabled the account.
func (u *User) IsSuspended() bool {
	return u.Status == UserStatusSuspended
}

// EmailVerified reports whether the address has been confirmed.
func (u *User) EmailVerified() bool {
	return u.EmailVerifiedAt != nil
}

// NormalizeEmail lower-cases and trims an address for storage and lookup.
//
// Addresses are compared case-insensitively in practice, and storing them as
// typed means "User@example.com" and "user@example.com" become two accounts
// that a unique index cannot tell apart. Normalising on the way in makes the
// index do what everyone assumes it already does.
//
// Only the case is touched. Stripping dots or +suffixes is provider-specific
// folklore, and applying Gmail's rules to every domain silently merges accounts
// that are genuinely distinct.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// NormalizeUsername applies the same rule to usernames.
func NormalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

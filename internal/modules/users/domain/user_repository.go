// File: internal/modules/users/domain/user_repository.go

package domain

import (
	"context"

	"github.com/google/uuid"
)

// UserRepository is the persistence port for User. The GORM adapter in infra/
// is the only implementation the application ships; tests use the generated
// mock.
//
// **Two conventions divide these methods, and mixing them up is the usual
// source of confusion.** A `Find*` that looks something up returns
// `(nil, nil)` when there is no such row: "is this email taken?" has *no* as an
// ordinary answer, and a caller hoping for a free address should not have to
// unwrap an error to hear it. A `Get*` that addresses one specific row returns
// a NOT_FOUND AppError, because there the row is a precondition. See
// docs/ARCHITECTURE.md, "Absence is not always an error".
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, user *User) error

	// GetByID returns NOT_FOUND when the id does not exist.
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)

	// FindByEmail returns (nil, nil) when no account has that address. The
	// argument is normalised by the adapter, so callers need not.
	FindByEmail(ctx context.Context, email string) (*User, error)

	// FindByUsername returns (nil, nil) when no account has that username.
	FindByUsername(ctx context.Context, username string) (*User, error)

	// FindByIdentifier resolves either an email address or a username, which is
	// what lets one login field accept both. It returns (nil, nil) for neither.
	FindByIdentifier(ctx context.Context, identifier string) (*User, error)

	// List returns a page of accounts, newest first, along with the total count
	// so a caller can render pagination.
	List(ctx context.Context, page Page) ([]*User, int64, error)
}

// Page is an offset/limit window. It is a struct rather than two ints so that
// adding a sort or a filter later does not change every call site.
type Page struct {
	Limit  int
	Offset int
}

// DefaultPageLimit and MaxPageLimit bound what a client may ask for. An
// unbounded limit is a denial-of-service primitive dressed up as a query
// parameter: one request for every row is all it takes.
const (
	DefaultPageLimit = 20
	MaxPageLimit     = 100
)

// Normalize clamps a requested page into the allowed range.
func (p Page) Normalize() Page {
	var normalized Page = p
	if normalized.Limit <= 0 {
		normalized.Limit = DefaultPageLimit
	}
	if normalized.Limit > MaxPageLimit {
		normalized.Limit = MaxPageLimit
	}
	if normalized.Offset < 0 {
		normalized.Offset = 0
	}
	return normalized
}

// SuspensionRegistry is notified when an account's status changes.
//
// It is a port because the thing behind it is the RBAC module's in-memory
// registry, and this module must not depend on that module's internals — but
// the status change happens here, and it has to reach memory immediately.
// Writing "suspended" to a row while every outstanding token keeps working is
// not a suspension.
//
// Implementations must be safe for concurrent use and must not block: this is
// called while handling a request.
type SuspensionRegistry interface {
	Suspend(userID uuid.UUID)
	Restore(userID uuid.UUID)
}

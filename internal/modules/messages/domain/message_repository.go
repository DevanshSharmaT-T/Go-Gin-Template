// File: internal/modules/messages/domain/message_repository.go

package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Page is an offset/limit window, bounded so an unbounded limit cannot be used
// to ask for every row in one request.
type Page struct {
	Limit  int
	Offset int
}

// Page bounds.
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

// NotificationRepository is the persistence port for notifications.
//
// **Every method that reads or writes one notification takes the user id as
// well as the notification id.** That is not redundancy: it is what makes a
// caller unable to mark somebody else's notification as read by guessing an
// id. The predicate is in the query rather than in an ownership check the
// handler has to remember.
type NotificationRepository interface {
	Create(ctx context.Context, notification *Notification) error

	// ListForUser returns one page of an account's notifications, newest first,
	// with the total and the unread count.
	ListForUser(ctx context.Context, userID uuid.UUID, page Page) ([]*Notification, int64, int64, error)

	// UnreadCount is the badge number.
	UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error)

	// MarkRead marks one notification read, and reports whether it belonged to
	// the user and was previously unread.
	MarkRead(ctx context.Context, userID uuid.UUID, id uuid.UUID, at time.Time) (bool, error)

	// MarkAllRead marks every unread notification read, returning how many.
	MarkAllRead(ctx context.Context, userID uuid.UUID, at time.Time) (int64, error)
}

// OutboundMailRepository is the persistence port for delivery records.
type OutboundMailRepository interface {
	Create(ctx context.Context, record *OutboundMail) error

	// List returns one page of records, newest first, with the total.
	List(ctx context.Context, page Page) ([]*OutboundMail, int64, error)
}

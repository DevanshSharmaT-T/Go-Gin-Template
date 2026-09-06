// File: internal/modules/messages/domain/notification_model.go

package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// NotificationKind is a coarse category, so a client can pick an icon and a
// colour without parsing the title.
type NotificationKind string

const (
	KindInfo    NotificationKind = "info"
	KindSuccess NotificationKind = "success"
	KindWarning NotificationKind = "warning"
	KindError   NotificationKind = "error"
)

// Valid reports whether k is one of the declared kinds, so a request body
// cannot write an arbitrary string into the column.
func (k NotificationKind) Valid() bool {
	switch k {
	case KindInfo, KindSuccess, KindWarning, KindError:
		return true
	}
	return false
}

// String makes NotificationKind printable.
func (k NotificationKind) String() string { return string(k) }

// Notification is an in-app message for one account.
//
// It is deliberately not a copy of an email. Email is for things that must
// reach someone who is not looking at the application; this is for things worth
// seeing when they next are. Sending both for the same event is a decision for
// the caller, not a coupling built in here.
type Notification struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_notifications_user_created,priority:1"`

	Kind  NotificationKind `gorm:"not null;type:varchar(20);default:'info'"`
	Title string           `gorm:"not null;type:varchar(200)"`
	Body  string           `gorm:"type:text"`

	// ReadAt is nil until the recipient has seen it. It is a timestamp rather
	// than a boolean because "when did they see this" is the question that
	// actually gets asked later, and a boolean cannot answer it.
	ReadAt *time.Time

	// The composite index is (user_id, created_at DESC): every read of this
	// table is one account's most recent notifications, and without it that is
	// a scan of everyone's.
	CreatedAt time.Time `gorm:"autoCreateTime;index:idx_notifications_user_created,priority:2,sort:desc"`
}

// TableName pins the table name.
func (Notification) TableName() string { return "notifications" }

// Read reports whether the recipient has seen it.
func (n *Notification) Read() bool { return n.ReadAt != nil }

// MaxTitleLength and MaxBodyLength bound what may be stored.
const (
	MaxTitleLength = 200
	MaxBodyLength  = 4000
)

// Notifier is the port other modules depend on to raise a notification.
//
// It exists so that a module wanting to tell someone something does not have to
// know how notifications are stored, and — more usefully — so that a module can
// be tested without one. It takes a context and returns an error like any other
// write; a caller that considers the notification optional logs the error and
// carries on, which is the usual choice.
type Notifier interface {
	Notify(ctx context.Context, userID uuid.UUID, kind NotificationKind, title string, body string) error
}

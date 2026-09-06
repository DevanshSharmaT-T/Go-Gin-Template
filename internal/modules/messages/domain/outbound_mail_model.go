// File: internal/modules/messages/domain/outbound_mail_model.go

package domain

import (
	"time"

	"github.com/google/uuid"
)

// DeliveryStatus is the outcome of one send attempt.
type DeliveryStatus string

const (
	// StatusSent means the mail server accepted the message. It is not a
	// promise of delivery — nothing available to the sender is — only that
	// responsibility passed to the server.
	StatusSent DeliveryStatus = "sent"
	// StatusFailed means it did not.
	StatusFailed DeliveryStatus = "failed"
)

// String makes DeliveryStatus printable.
func (s DeliveryStatus) String() string { return string(s) }

// OutboundMail records one attempt to send an email.
//
// **Why keep this at all.** "Did they get the reset link?" is the single most
// common support question a system like this produces, and without a record the
// only answer is a guess. It is also the audit trail for the opposite question:
// which addresses this system has sent to, and when.
//
// **What it deliberately does not store is the body.** Every message this
// application sends contains a single-use link, and a table of message bodies
// is a table of working credentials — the exact mistake the token fingerprint
// design avoids elsewhere. The subject and the recipient are enough to answer
// the question; the link is not.
type OutboundMail struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`

	Recipient string `gorm:"not null;type:varchar(255);index"`
	Subject   string `gorm:"not null;type:varchar(255)"`

	Status DeliveryStatus `gorm:"not null;type:varchar(20);index"`

	// Failure is the operator-facing reason, empty when Status is sent. It is
	// the classified message rather than the driver's, for the same reason
	// nothing else lets driver text travel.
	Failure string `gorm:"type:varchar(500)"`

	CreatedAt time.Time `gorm:"autoCreateTime;index:idx_outbound_mail_created,sort:desc"`
}

// TableName pins the table name.
func (OutboundMail) TableName() string { return "outbound_mail" }

// Succeeded reports whether the mail server accepted the message.
func (m *OutboundMail) Succeeded() bool { return m.Status == StatusSent }

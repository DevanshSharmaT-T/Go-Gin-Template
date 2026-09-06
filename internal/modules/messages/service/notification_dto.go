// File: internal/modules/messages/service/notification_dto.go

package service

import (
	"time"

	"github.com/google/uuid"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/domain"
)

// NotificationResponseDTO is the public shape of a notification.
type NotificationResponseDTO struct {
	ID        uuid.UUID  `json:"id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Read      bool       `json:"read"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// NotificationListResponseDTO is one page of an account's notifications.
type NotificationListResponseDTO struct {
	Notifications []*NotificationResponseDTO `json:"notifications"`
	Total         int64                      `json:"total"`
	Unread        int64                      `json:"unread"`
	Limit         int                        `json:"limit"`
	Offset        int                        `json:"offset"`
}

// UnreadCountResponseDTO is the badge number on its own, so a client polling
// for it does not have to fetch a page it will discard.
type UnreadCountResponseDTO struct {
	Unread int64 `json:"unread"`
}

// SendNotificationRequestDTO is the administrative "tell this account
// something" payload.
type SendNotificationRequestDTO struct {
	UserID uuid.UUID `json:"user_id" binding:"required"`
	Kind   string    `json:"kind"`
	Title  string    `json:"title" binding:"required"`
	Body   string    `json:"body"`
}

// OutboundMailResponseDTO is one delivery record.
//
// There is no body field, and that is the point of the record's design: the
// message contained a single-use link, and a list endpoint returning bodies
// would be a list endpoint returning working credentials.
type OutboundMailResponseDTO struct {
	ID        uuid.UUID `json:"id"`
	Recipient string    `json:"recipient"`
	Subject   string    `json:"subject"`
	Status    string    `json:"status"`
	Failure   string    `json:"failure,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// OutboundMailListResponseDTO is one page of delivery records.
type OutboundMailListResponseDTO struct {
	Mail   []*OutboundMailResponseDTO `json:"mail"`
	Total  int64                      `json:"total"`
	Limit  int                        `json:"limit"`
	Offset int                        `json:"offset"`
}

// toNotificationResponse maps an entity to its public shape.
func toNotificationResponse(n *domain.Notification) *NotificationResponseDTO {
	if n == nil {
		return nil
	}
	return &NotificationResponseDTO{
		ID:        n.ID,
		Kind:      n.Kind.String(),
		Title:     n.Title,
		Body:      n.Body,
		Read:      n.Read(),
		ReadAt:    n.ReadAt,
		CreatedAt: n.CreatedAt,
	}
}

// toOutboundMailResponse maps a delivery record to its public shape.
func toOutboundMailResponse(m *domain.OutboundMail) *OutboundMailResponseDTO {
	if m == nil {
		return nil
	}
	return &OutboundMailResponseDTO{
		ID:        m.ID,
		Recipient: m.Recipient,
		Subject:   m.Subject,
		Status:    m.Status.String(),
		Failure:   m.Failure,
		CreatedAt: m.CreatedAt,
	}
}

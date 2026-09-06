// File: internal/modules/messages/service/notification_service.go

package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// NotificationService owns in-app notifications and the outbound-mail log.
type NotificationService struct {
	notifications domain.NotificationRepository
	mail          domain.OutboundMailRepository
}

// NewNotificationService builds the service.
func NewNotificationService(
	notifications domain.NotificationRepository,
	mail domain.OutboundMailRepository,
) *NotificationService {
	return &NotificationService{notifications: notifications, mail: mail}
}

// Notify raises a notification for an account. It is the domain.Notifier
// implementation, so any module can depend on the port rather than on this.
func (s *NotificationService) Notify(
	ctx context.Context,
	userID uuid.UUID,
	kind domain.NotificationKind,
	title string,
	body string,
) error {
	if userID == uuid.Nil {
		return errors.NewValidationError("a notification needs a recipient", nil)
	}
	if !kind.Valid() {
		kind = domain.KindInfo
	}

	var err error = validateText(title, body)
	if err != nil {
		return err
	}

	return s.notifications.Create(ctx, &domain.Notification{
		UserID: userID,
		Kind:   kind,
		Title:  strings.TrimSpace(title),
		Body:   strings.TrimSpace(body),
	})
}

// Send is the administrative entry point behind POST /api/notifications.
func (s *NotificationService) Send(
	ctx context.Context,
	req *SendNotificationRequestDTO,
) (*NotificationResponseDTO, error) {
	var kind domain.NotificationKind = domain.NotificationKind(req.Kind)
	if req.Kind == "" {
		kind = domain.KindInfo
	}
	if !kind.Valid() {
		return nil, errors.NewValidationError(
			"kind must be one of info, success, warning, error", nil).
			WithDetail("kind", "must be one of info, success, warning, error")
	}

	var err error = s.Notify(ctx, req.UserID, kind, req.Title, req.Body)
	if err != nil {
		return nil, err
	}

	return &NotificationResponseDTO{
		Kind:  kind.String(),
		Title: strings.TrimSpace(req.Title),
		Body:  strings.TrimSpace(req.Body),
	}, nil
}

// List returns one page of the caller's own notifications.
func (s *NotificationService) List(
	ctx context.Context,
	userID uuid.UUID,
	page domain.Page,
) (*NotificationListResponseDTO, error) {
	var normalized domain.Page = page.Normalize()

	var notifications []*domain.Notification
	var total int64
	var unread int64
	var err error
	notifications, total, unread, err = s.notifications.ListForUser(ctx, userID, normalized)
	if err != nil {
		return nil, err
	}

	var response *NotificationListResponseDTO = &NotificationListResponseDTO{
		Notifications: make([]*NotificationResponseDTO, 0, len(notifications)),
		Total:         total,
		Unread:        unread,
		Limit:         normalized.Limit,
		Offset:        normalized.Offset,
	}

	var notification *domain.Notification
	for _, notification = range notifications {
		response.Notifications = append(response.Notifications, toNotificationResponse(notification))
	}
	return response, nil
}

// UnreadCount returns the badge number for an account.
func (s *NotificationService) UnreadCount(
	ctx context.Context,
	userID uuid.UUID,
) (*UnreadCountResponseDTO, error) {
	var count int64
	var err error
	count, err = s.notifications.UnreadCount(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &UnreadCountResponseDTO{Unread: count}, nil
}

// MarkRead marks one of the caller's own notifications read.
//
// A notification belonging to someone else and one that does not exist produce
// the same NOT_FOUND. Distinguishing them would confirm that a given id exists,
// which is the one thing a caller guessing ids wants to learn.
func (s *NotificationService) MarkRead(
	ctx context.Context,
	userID uuid.UUID,
	id uuid.UUID,
) error {
	var updated bool
	var err error
	updated, err = s.notifications.MarkRead(ctx, userID, id, time.Now().UTC())
	if err != nil {
		return err
	}
	if !updated {
		return errors.NewNotFoundError("notification not found", nil)
	}
	return nil
}

// MarkAllRead clears the caller's badge.
func (s *NotificationService) MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	return s.notifications.MarkAllRead(ctx, userID, time.Now().UTC())
}

// ListOutboundMail returns the delivery log.
func (s *NotificationService) ListOutboundMail(
	ctx context.Context,
	page domain.Page,
) (*OutboundMailListResponseDTO, error) {
	var normalized domain.Page = page.Normalize()

	var records []*domain.OutboundMail
	var total int64
	var err error
	records, total, err = s.mail.List(ctx, normalized)
	if err != nil {
		return nil, err
	}

	var response *OutboundMailListResponseDTO = &OutboundMailListResponseDTO{
		Mail:   make([]*OutboundMailResponseDTO, 0, len(records)),
		Total:  total,
		Limit:  normalized.Limit,
		Offset: normalized.Offset,
	}

	var record *domain.OutboundMail
	for _, record = range records {
		response.Mail = append(response.Mail, toOutboundMailResponse(record))
	}
	return response, nil
}

// validateText bounds the stored strings.
func validateText(title string, body string) error {
	var trimmedTitle string = strings.TrimSpace(title)
	if trimmedTitle == "" {
		return errors.NewValidationError("a notification needs a title", nil).
			WithDetail("title", "is required")
	}
	if utf8.RuneCountInString(trimmedTitle) > domain.MaxTitleLength {
		return errors.NewValidationError("the notification title is too long", nil).
			WithDetail("title", "must be at most 200 characters")
	}
	if utf8.RuneCountInString(body) > domain.MaxBodyLength {
		return errors.NewValidationError("the notification body is too long", nil).
			WithDetail("body", "must be at most 4000 characters")
	}
	return nil
}

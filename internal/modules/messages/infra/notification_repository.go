// File: internal/modules/messages/infra/notification_repository.go

package infra

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database"
)

// GormNotificationRepository is the GORM adapter for
// domain.NotificationRepository.
type GormNotificationRepository struct {
	db *gorm.DB
}

// NewGormNotificationRepository builds the adapter, declaring the interface as
// its return type so fx binds it there.
func NewGormNotificationRepository(db *gorm.DB) domain.NotificationRepository {
	return &GormNotificationRepository{db: db}
}

// Create stores a notification.
func (r *GormNotificationRepository) Create(ctx context.Context, notification *domain.Notification) error {
	var err error = r.db.WithContext(ctx).Create(notification).Error
	if err != nil {
		return database.TranslateError(err)
	}
	return nil
}

// ListForUser returns one page, plus the total and unread counts.
func (r *GormNotificationRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
	page domain.Page,
) ([]*domain.Notification, int64, int64, error) {
	var normalized domain.Page = page.Normalize()

	var total int64
	var err error = r.db.WithContext(ctx).
		Model(&domain.Notification{}).
		Where("user_id = ?", userID).
		Count(&total).Error
	if err != nil {
		return nil, 0, 0, database.TranslateError(err)
	}

	var unread int64
	unread, err = r.UnreadCount(ctx, userID)
	if err != nil {
		return nil, 0, 0, err
	}

	var notifications []*domain.Notification
	err = r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(normalized.Limit).
		Offset(normalized.Offset).
		Find(&notifications).Error
	if err != nil {
		return nil, 0, 0, database.TranslateError(err)
	}

	return notifications, total, unread, nil
}

// UnreadCount counts the unseen notifications for an account.
func (r *GormNotificationRepository) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	var count int64
	var err error = r.db.WithContext(ctx).
		Model(&domain.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Count(&count).Error
	if err != nil {
		return 0, database.TranslateError(err)
	}
	return count, nil
}

// MarkRead marks one notification read, reporting whether this call did it.
//
// The user id is in the WHERE clause, not in a check the caller performs. That
// is what makes marking someone else's notification read impossible rather than
// merely unimplemented — the query simply matches nothing.
func (r *GormNotificationRepository) MarkRead(
	ctx context.Context,
	userID uuid.UUID,
	id uuid.UUID,
	at time.Time,
) (bool, error) {
	var result *gorm.DB = r.db.WithContext(ctx).
		Model(&domain.Notification{}).
		Where("id = ? AND user_id = ? AND read_at IS NULL", id, userID).
		Update("read_at", at)

	if result.Error != nil {
		return false, database.TranslateError(result.Error)
	}
	return result.RowsAffected == 1, nil
}

// MarkAllRead marks every unread notification read.
func (r *GormNotificationRepository) MarkAllRead(
	ctx context.Context,
	userID uuid.UUID,
	at time.Time,
) (int64, error) {
	var result *gorm.DB = r.db.WithContext(ctx).
		Model(&domain.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Update("read_at", at)

	if result.Error != nil {
		return 0, database.TranslateError(result.Error)
	}
	return result.RowsAffected, nil
}

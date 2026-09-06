// File: internal/modules/messages/infra/outbound_mail_repository.go

package infra

import (
	"context"

	"gorm.io/gorm"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database"
)

// GormOutboundMailRepository is the GORM adapter for
// domain.OutboundMailRepository.
type GormOutboundMailRepository struct {
	db *gorm.DB
}

// NewGormOutboundMailRepository builds the adapter.
func NewGormOutboundMailRepository(db *gorm.DB) domain.OutboundMailRepository {
	return &GormOutboundMailRepository{db: db}
}

// Create stores one delivery record.
func (r *GormOutboundMailRepository) Create(ctx context.Context, record *domain.OutboundMail) error {
	var err error = r.db.WithContext(ctx).Create(record).Error
	if err != nil {
		return database.TranslateError(err)
	}
	return nil
}

// List returns one page of records, newest first.
func (r *GormOutboundMailRepository) List(
	ctx context.Context,
	page domain.Page,
) ([]*domain.OutboundMail, int64, error) {
	var normalized domain.Page = page.Normalize()

	var total int64
	var err error = r.db.WithContext(ctx).Model(&domain.OutboundMail{}).Count(&total).Error
	if err != nil {
		return nil, 0, database.TranslateError(err)
	}

	var records []*domain.OutboundMail
	err = r.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(normalized.Limit).
		Offset(normalized.Offset).
		Find(&records).Error
	if err != nil {
		return nil, 0, database.TranslateError(err)
	}

	return records, total, nil
}

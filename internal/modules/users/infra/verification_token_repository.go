// File: internal/modules/users/infra/verification_token_repository.go

package infra

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/crypt"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// GormVerificationTokenRepository is the GORM adapter for
// domain.VerificationTokenRepository.
type GormVerificationTokenRepository struct {
	db *gorm.DB
}

// NewGormVerificationTokenRepository builds the adapter. The return type is the
// interface, for the reason given on NewGormUserRepository.
func NewGormVerificationTokenRepository(db *gorm.DB) domain.VerificationTokenRepository {
	return &GormVerificationTokenRepository{db: db}
}

// Create records an issued token.
func (r *GormVerificationTokenRepository) Create(ctx context.Context, token *domain.VerificationToken) error {
	var err error = r.db.WithContext(ctx).Create(token).Error
	if err != nil {
		return database.TranslateError(err)
	}
	return nil
}

// FindByFingerprint returns (nil, nil) for a token that was never issued.
func (r *GormVerificationTokenRepository) FindByFingerprint(
	ctx context.Context,
	fingerprint string,
) (*domain.VerificationToken, error) {
	var token domain.VerificationToken
	var err error = r.db.WithContext(ctx).
		Where("fingerprint = ?", fingerprint).
		First(&token).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, database.TranslateError(err)
	}
	return &token, nil
}

// Consume marks a token redeemed, and reports whether this call is the one that
// did it.
//
// The `consumed_at IS NULL` predicate is the whole point. Reading the token,
// deciding it is usable and then updating is a check-then-act race: two
// requests carrying the same link can both pass the check before either writes.
// Here the database decides, and RowsAffected says who won — so a reset link
// clicked twice sets the password once.
func (r *GormVerificationTokenRepository) Consume(
	ctx context.Context,
	id uuid.UUID,
	at time.Time,
) (bool, error) {
	var result *gorm.DB = r.db.WithContext(ctx).
		Model(&domain.VerificationToken{}).
		Where("id = ? AND consumed_at IS NULL", id).
		Update("consumed_at", at)

	if result.Error != nil {
		return false, database.TranslateError(result.Error)
	}
	return result.RowsAffected == 1, nil
}

// InvalidateOutstanding retires every unredeemed token a user holds for a
// purpose, so issuing a new link cancels the previous one.
func (r *GormVerificationTokenRepository) InvalidateOutstanding(
	ctx context.Context,
	userID uuid.UUID,
	purpose crypt.Purpose,
	at time.Time,
) error {
	var err error = r.db.WithContext(ctx).
		Model(&domain.VerificationToken{}).
		Where("user_id = ? AND purpose = ? AND consumed_at IS NULL", userID, purpose).
		Update("consumed_at", at).Error
	if err != nil {
		return database.TranslateError(err)
	}
	return nil
}

// DeleteExpired removes tokens past their expiry and reports how many went.
func (r *GormVerificationTokenRepository) DeleteExpired(
	ctx context.Context,
	before time.Time,
) (int64, error) {
	var result *gorm.DB = r.db.WithContext(ctx).
		Where("expires_at < ?", before).
		Delete(&domain.VerificationToken{})
	if result.Error != nil {
		return 0, database.TranslateError(result.Error)
	}
	return result.RowsAffected, nil
}

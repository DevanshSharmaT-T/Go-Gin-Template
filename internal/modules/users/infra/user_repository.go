// File: internal/modules/users/infra/user_repository.go

package infra

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// GormUserRepository is the GORM adapter for domain.UserRepository.
type GormUserRepository struct {
	db *gorm.DB
}

// NewGormUserRepository builds the adapter.
//
// **The declared return type is the interface, and that is not a style
// choice.** fx binds a constructor's result under the type it declares. Return
// *GormUserRepository here and nothing depending on domain.UserRepository can
// resolve, with a "missing type" error at startup rather than a compile error.
func NewGormUserRepository(db *gorm.DB) domain.UserRepository {
	return &GormUserRepository{db: db}
}

// Create inserts a new account.
func (r *GormUserRepository) Create(ctx context.Context, user *domain.User) error {
	var err error = r.db.WithContext(ctx).Create(user).Error
	if err != nil {
		return database.TranslateError(err)
	}
	return nil
}

// Update writes every column of an existing account.
//
// Save is used rather than Updates so that clearing a field — setting
// LastLoginAt back to nil, say — actually persists. Updates skips zero values,
// which makes "write this struct" quietly mean "write the non-empty parts of
// this struct".
func (r *GormUserRepository) Update(ctx context.Context, user *domain.User) error {
	var err error = r.db.WithContext(ctx).Save(user).Error
	if err != nil {
		return database.TranslateError(err)
	}
	return nil
}

// GetByID returns the account, or a NOT_FOUND AppError.
func (r *GormUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	var user domain.User
	var err error = r.db.WithContext(ctx).First(&user, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.NewNotFoundError("user not found", err)
		}
		return nil, database.TranslateError(err)
	}
	return &user, nil
}

// FindByEmail returns (nil, nil) when no account has that address.
func (r *GormUserRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.findOne(ctx, "email = ?", domain.NormalizeEmail(email))
}

// FindByUsername returns (nil, nil) when no account has that username.
func (r *GormUserRepository) FindByUsername(ctx context.Context, username string) (*domain.User, error) {
	return r.findOne(ctx, "username = ?", domain.NormalizeUsername(username))
}

// FindByIdentifier resolves an email address or a username in one query, which
// is what lets the login form take either.
//
// Both sides are normalised the same way they are on write, so the comparison
// is a plain index lookup rather than a lower() over the table.
func (r *GormUserRepository) FindByIdentifier(ctx context.Context, identifier string) (*domain.User, error) {
	var normalized string = domain.NormalizeEmail(identifier)
	return r.findOne(ctx, "email = ? OR username = ?", normalized, normalized)
}

// List returns one page of accounts, newest first, with the total count.
func (r *GormUserRepository) List(ctx context.Context, page domain.Page) ([]*domain.User, int64, error) {
	var normalized domain.Page = page.Normalize()

	var total int64
	var err error = r.db.WithContext(ctx).Model(&domain.User{}).Count(&total).Error
	if err != nil {
		return nil, 0, database.TranslateError(err)
	}

	var users []*domain.User
	err = r.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(normalized.Limit).
		Offset(normalized.Offset).
		Find(&users).Error
	if err != nil {
		return nil, 0, database.TranslateError(err)
	}

	return users, total, nil
}

// findOne is the shared body of the Find* lookups: one row or (nil, nil).
func (r *GormUserRepository) findOne(ctx context.Context, query string, args ...any) (*domain.User, error) {
	var user domain.User
	var err error = r.db.WithContext(ctx).Where(query, args...).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Absence is the answer, not a failure. See the port's doc comment.
			return nil, nil
		}
		return nil, database.TranslateError(err)
	}
	return &user, nil
}

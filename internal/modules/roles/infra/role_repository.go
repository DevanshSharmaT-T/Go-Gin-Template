// File: internal/modules/roles/infra/role_repository.go

package infra

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// GormRoleRepository is the GORM adapter for domain.RoleRepository.
type GormRoleRepository struct {
	db *gorm.DB
}

// NewGormRoleRepository builds the adapter. The declared return type is the
// interface, which is what fx binds on.
func NewGormRoleRepository(db *gorm.DB) domain.RoleRepository {
	return &GormRoleRepository{db: db}
}

// GetByID returns the role, or a NOT_FOUND AppError.
func (r *GormRoleRepository) GetByID(ctx context.Context, id int64) (*domain.Role, error) {
	var role domain.Role
	var err error = r.db.WithContext(ctx).First(&role, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.NewNotFoundError("role not found", err)
		}
		return nil, database.TranslateError(err)
	}
	return &role, nil
}

// List returns every role, most senior first.
func (r *GormRoleRepository) List(ctx context.Context) ([]*domain.Role, error) {
	var roles []*domain.Role
	var err error = r.db.WithContext(ctx).
		Order("hierarchy_level ASC").
		Find(&roles).Error
	if err != nil {
		return nil, database.TranslateError(err)
	}
	return roles, nil
}

// PermissionsFor returns the slugs a role grants.
func (r *GormRoleRepository) PermissionsFor(ctx context.Context, roleID int64) ([]string, error) {
	var slugs []string
	var err error = r.db.WithContext(ctx).
		Table("role_permissions AS rp").
		Joins("JOIN permissions AS p ON p.id = rp.permission_id").
		Where("rp.role_id = ?", roleID).
		Order("p.slug ASC").
		Pluck("p.slug", &slugs).Error
	if err != nil {
		return nil, database.TranslateError(err)
	}
	return slugs, nil
}

// ListPermissions returns every defined permission.
func (r *GormRoleRepository) ListPermissions(ctx context.Context) ([]*domain.Permission, error) {
	var permissions []*domain.Permission
	var err error = r.db.WithContext(ctx).Order("slug ASC").Find(&permissions).Error
	if err != nil {
		return nil, database.TranslateError(err)
	}
	return permissions, nil
}

// ReplaceGrants sets a role's permissions and bumps its version atomically.
func (r *GormRoleRepository) ReplaceGrants(
	ctx context.Context,
	roleID int64,
	slugs []string,
) (int64, error) {
	var version int64

	var err error = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Resolve the slugs first, so an unknown one fails before anything is
		// deleted. Granting a subset of what was asked for would be worse than
		// refusing: the caller would believe the whole set had been applied.
		var ids []int64
		if len(slugs) > 0 {
			var txErr error = tx.Model(&domain.Permission{}).
				Where("slug IN ?", slugs).
				Pluck("id", &ids).Error
			if txErr != nil {
				return txErr
			}
			if len(ids) != len(slugs) {
				return errors.NewValidationError(
					"one or more permissions do not exist", nil).
					WithDetail("permissions", "must all be slugs returned by GET /api/roles/permissions")
			}
		}

		var txErr error = tx.Where("role_id = ?", roleID).
			Delete(&domain.RolePermission{}).Error
		if txErr != nil {
			return txErr
		}

		if len(ids) > 0 {
			var grants []domain.RolePermission = make([]domain.RolePermission, 0, len(ids))
			var id int64
			for _, id = range ids {
				grants = append(grants, domain.RolePermission{RoleID: roleID, PermissionID: id})
			}
			txErr = tx.Create(&grants).Error
			if txErr != nil {
				return txErr
			}
		}

		// The bump is what invalidates every outstanding token for this role,
		// and it shares the transaction with the change it is reporting.
		txErr = tx.Model(&domain.Role{}).
			Where("id = ?", roleID).
			UpdateColumn("perm_version", gorm.Expr("perm_version + 1")).Error
		if txErr != nil {
			return txErr
		}

		return tx.Model(&domain.Role{}).
			Where("id = ?", roleID).
			Select("perm_version").
			Scan(&version).Error
	})

	if err != nil {
		var appErr *errors.AppError
		if errors.As(err, &appErr) {
			return 0, appErr
		}
		return 0, database.TranslateError(err)
	}

	return version, nil
}

// Snapshot reads every role and every grant, for the boot warm-up.
func (r *GormRoleRepository) Snapshot(ctx context.Context) (*domain.Snapshot, error) {
	var roles []domain.Role
	var err error = r.db.WithContext(ctx).Find(&roles).Error
	if err != nil {
		return nil, database.TranslateError(err)
	}

	// One join for every grant in the system. The registry is rebuilt whole,
	// so reading it per role would be N queries for the same answer.
	var grants []domain.Grant
	err = r.db.WithContext(ctx).
		Table("role_permissions AS rp").
		Select("rp.role_id AS role_id, p.slug AS slug").
		Joins("JOIN permissions AS p ON p.id = rp.permission_id").
		Scan(&grants).Error
	if err != nil {
		return nil, database.TranslateError(err)
	}

	var suspended []uuid.UUID
	suspended, err = r.SuspendedUserIDs(ctx)
	if err != nil {
		return nil, err
	}

	return &domain.Snapshot{Roles: roles, Grants: grants, SuspendedUsers: suspended}, nil
}

// SuspendedUserIDs lists suspended accounts.
//
// The users table is read directly rather than through the users repository:
// this is one column with one predicate, and the alternative is a cross-module
// dependency that exists only to avoid writing a table name.
func (r *GormRoleRepository) SuspendedUserIDs(ctx context.Context) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	var err error = r.db.WithContext(ctx).
		Table("users").
		Where("status = ?", "suspended").
		Pluck("id", &ids).Error
	if err != nil {
		return nil, database.TranslateError(err)
	}
	return ids, nil
}

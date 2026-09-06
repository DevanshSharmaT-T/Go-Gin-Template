// File: internal/modules/roles/seeds/permission_seeder.go

package seeds

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database"
)

// NewPermissionSeeder inserts the permission catalogue and the initial grants.
func NewPermissionSeeder() database.SeedFunc {
	return seedPermissions
}

// seedPermissions inserts every permission the application defines, then grants
// each role its starting set.
//
// **The slugs come from the same constants the routes are gated on.** That is
// the point of the catalogue: the classic failure here is a route gated on
// `case:view` while the seeder inserts `cases:view`, which fails closed and
// silently removes access from everyone who should have had it. Two hand-typed
// strings in two files is exactly the check people are bad at, so there is only
// one string.
//
// Grants are inserted with DoNothing, so a permission an administrator has
// revoked through the API is not silently restored on the next restart.
func seedPermissions(db *gorm.DB) error {
	var definitions []domain.PermissionDefinition = domain.Catalogue()
	var permissions []domain.Permission = make([]domain.Permission, 0, len(definitions))

	var definition domain.PermissionDefinition
	for _, definition = range definitions {
		permissions = append(permissions, domain.Permission{
			Slug:        definition.Slug,
			Description: definition.Description,
		})
	}

	var err error = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&permissions).Error
	if err != nil {
		return err
	}

	// Resolve the slugs to ids in one query rather than per grant.
	var rows []domain.Permission
	err = db.Model(&domain.Permission{}).Find(&rows).Error
	if err != nil {
		return err
	}

	var idBySlug map[string]int64 = make(map[string]int64, len(rows))
	var row domain.Permission
	for _, row = range rows {
		idBySlug[row.Slug] = row.ID
	}

	var grants []domain.RolePermission
	var roleDefinition domain.RoleDefinition
	for _, roleDefinition = range domain.Roles() {
		var slug string
		for _, slug = range roleDefinition.Grants {
			var id int64
			var known bool
			id, known = idBySlug[slug]
			if !known {
				// Unreachable while the grants reference catalogue constants,
				// which is the design. Skipping rather than failing keeps a
				// half-applied catalogue from blocking every boot.
				continue
			}
			grants = append(grants, domain.RolePermission{
				RoleID:       roleDefinition.ID,
				PermissionID: id,
			})
		}
	}

	if len(grants) == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&grants).Error
}

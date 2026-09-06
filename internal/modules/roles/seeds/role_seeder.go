// File: internal/modules/roles/seeds/role_seeder.go

// Package seeds inserts the rows an empty database needs before anyone can sign
// in: the roles, the permissions they can grant, and optionally a first
// administrator.
//
// Every seeder here runs on every boot and is idempotent, keyed on a natural
// unique column with ON CONFLICT DO NOTHING. That is one statement and one
// round trip, and — unlike counting first and then inserting — it does not lose
// to a second process starting at the same moment.
package seeds

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database"
)

// NewRoleSeeder inserts the role hierarchy.
//
// The declared return type is database.SeedFunc, and it has to be: the value
// group collects that exact type, so a structurally identical
// func(*gorm.DB) error lands in a different slot where nothing collects it and
// simply never runs.
func NewRoleSeeder() database.SeedFunc {
	return seedRoles
}

// seedRoles inserts every role in the catalogue, leaving existing ones alone.
//
// DoNothing rather than an upsert is deliberate. A deployment that has renamed
// a role or changed its hierarchy level has made a decision, and a seeder that
// overwrote it on every restart would quietly undo that decision — and, by
// changing a level, alter what every outstanding token means.
func seedRoles(db *gorm.DB) error {
	var definitions []domain.RoleDefinition = domain.Roles()
	var roles []domain.Role = make([]domain.Role, 0, len(definitions))

	var definition domain.RoleDefinition
	for _, definition = range definitions {
		roles = append(roles, domain.Role{
			ID:             definition.ID,
			Code:           definition.Code,
			Name:           definition.Name,
			Description:    definition.Description,
			HierarchyLevel: definition.HierarchyLevel,
			PermVersion:    1,
		})
	}

	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&roles).Error
}

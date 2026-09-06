// File: internal/shared/database/migrations/migrations.go

// Package migrations holds the ordered, numbered schema changes that
// AutoMigrate cannot express.
//
// # Why there are two mechanisms
//
// Tables themselves come from the models registered in the fx `group:"models"`
// value group: AutoMigrate reads their GORM tags and creates or widens the
// table to match. That covers the common case — a new entity, a new column, a
// new index — with no file to edit and nothing to remember.
//
// AutoMigrate will never drop a column, rename one, change a type, backfill a
// value or add a constraint to data that already violates it. Those are what a
// numbered migration is for. The version number then identifies a schema state:
// "the database is at version 7" is a fact you can act on.
//
// # The rule that keeps version numbers meaningful
//
// A migration must describe the schema *as it was at that version*, so it
// declares a local anonymous struct and never references a domain model. If
// migration 3 referred to domain.User and someone later added a field to it,
// migration 3 would change meaning retroactively and a fresh database would
// stop reproducing the history. For the same reason a migration never touches
// the models registry.
//
// # Adding one
//
// Append the next integer key. Versions must be contiguous from 1: the runner
// checks before it applies anything and refuses to start if a number is
// missing, naming it. If two branches both add version 7, one renumbers when
// rebasing.
package migrations

import "gorm.io/gorm"

// MigrationFunc is one versioned schema change. It receives a transaction: on
// PostgreSQL, DDL is transactional, so a migration that fails halfway leaves no
// half-applied schema behind.
type MigrationFunc func(db *gorm.DB) error

// migrationMap is the ordered catalogue of schema changes.
//
// It ships empty on purpose. The template registers no models of its own, so
// there is no schema to version yet, and an example migration would create a
// table every adopter would then have to delete. Your first migration is
// version 1.
//
// The recipes below are the patterns worth having to hand. Copy one, renumber
// it, and keep the local-struct rule.
var migrationMap map[int]MigrationFunc = map[int]MigrationFunc{
	// --- Add a column with a default -------------------------------------
	//
	// 1: func(db *gorm.DB) error {
	//     type user struct {
	//         Bio string `gorm:"type:text;not null;default:''"`
	//     }
	//     return db.Table("users").Migrator().AddColumn(&user{}, "Bio")
	// },

	// --- Rename a column, preserving the data ----------------------------
	//
	// AutoMigrate cannot do this: it sees an unknown column and an absent one,
	// and adds the second while leaving the first behind with the data in it.
	//
	// 2: func(db *gorm.DB) error {
	//     type user struct{}
	//     return db.Table("users").Migrator().RenameColumn(&user{}, "phone", "mobile_number")
	// },

	// --- Add an index ----------------------------------------------------
	//
	// 3: func(db *gorm.DB) error {
	//     type user struct {
	//         Email string `gorm:"index:idx_users_email"`
	//     }
	//     return db.Table("users").Migrator().CreateIndex(&user{}, "idx_users_email")
	// },

	// --- Backfill, then tighten ------------------------------------------
	//
	// The order matters. Adding a NOT NULL column to a populated table fails;
	// adding it nullable, filling it in, then tightening it succeeds.
	//
	// 4: func(db *gorm.DB) error {
	//     type user struct {
	//         FullName string `gorm:"type:varchar(255)"`
	//     }
	//     var err error = db.Table("users").Migrator().AddColumn(&user{}, "FullName")
	//     if err != nil {
	//         return err
	//     }
	//     err = db.Exec(
	//         `UPDATE users SET full_name = first_name || ' ' || last_name ` +
	//             `WHERE full_name IS NULL`).Error
	//     if err != nil {
	//         return err
	//     }
	//     return db.Exec(`ALTER TABLE users ALTER COLUMN full_name SET NOT NULL`).Error
	// },

	// --- Add a constraint ------------------------------------------------
	//
	// This fails if existing rows violate it, which is the point: find out
	// during a deployment, not during the first request that relies on it.
	//
	// 5: func(db *gorm.DB) error {
	//     return db.Exec(
	//         `ALTER TABLE users ADD CONSTRAINT chk_users_email_lower ` +
	//             `CHECK (email = lower(email))`).Error
	// },

	// --- Change a column's type ------------------------------------------
	//
	// PostgreSQL needs the USING clause to know how to convert the existing
	// values, and GORM's migrator does not generate one.
	//
	// 6: func(db *gorm.DB) error {
	//     return db.Exec(
	//         `ALTER TABLE users ALTER COLUMN age TYPE integer USING (age::integer)`).Error
	// },

	// --- Drop a column ---------------------------------------------------
	//
	// Destructive and irreversible. Ship the code that stopped reading the
	// column first, and drop it in a later release.
	//
	// 7: func(db *gorm.DB) error {
	//     type user struct{}
	//     return db.Table("users").Migrator().DropColumn(&user{}, "deprecated_field")
	// },
}

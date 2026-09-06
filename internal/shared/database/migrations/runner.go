// File: internal/shared/database/migrations/runner.go

package migrations

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// Run applies every pending migration from the built-in catalogue. It is what
// the boot sequence calls, immediately after the models registry has been
// auto-migrated.
func Run(ctx context.Context, db *gorm.DB) error {
	return Apply(ctx, db, migrationMap)
}

// Apply applies the pending migrations in m.
//
// It is exported so that a test — or an entry point with its own catalogue —
// can drive the runner without adding a migration to the template. Everything
// [Run] does, it does through here.
//
// The sequence is: validate, read what has already been applied, then apply
// each missing version in ascending order inside its own transaction. Validation
// happens first and separately, so a numbering mistake is reported before any
// schema has changed rather than halfway through.
func Apply(ctx context.Context, db *gorm.DB, m map[int]MigrationFunc) error {
	var err error = validateContiguous(m)
	if err != nil {
		return err
	}

	var scoped *gorm.DB = db.WithContext(ctx)
	var log *zerolog.Logger = logger.FromContext(ctx)

	err = scoped.AutoMigrate(&MigrationRecord{})
	if err != nil {
		return errors.NewInternalError("could not create the migration_records table", err)
	}

	var applied map[int]bool
	applied, err = appliedVersions(scoped)
	if err != nil {
		return err
	}

	// Membership rather than "highest applied": if a rebase ever leaves a hole,
	// the hole is filled instead of being skipped forever because a later
	// version happens to be recorded.
	var pending []int
	var version int
	for version = 1; version <= len(m); version++ {
		if !applied[version] {
			pending = append(pending, version)
		}
	}

	if len(pending) == 0 {
		log.Debug().Int("version", len(m)).Msg("database schema is up to date")
		return nil
	}

	log.Info().Ints("pending", pending).Msg("applying migrations")

	for _, version = range pending {
		err = applyOne(scoped, version, m[version])
		if err != nil {
			return err
		}
		log.Info().Int("version", version).Msg("applied migration")
	}

	return nil
}

// appliedVersions reads the recorded versions into a set.
//
// The Pluck error is checked. Discarding it is the classic version of this bug:
// the read fails, the set comes back empty, and every migration is applied a
// second time against a database that already has them.
func appliedVersions(db *gorm.DB) (map[int]bool, error) {
	var versions []int
	var err error = db.Model(&MigrationRecord{}).Pluck("version", &versions).Error
	if err != nil {
		return nil, errors.NewInternalError("could not read applied migration versions", err)
	}

	var applied map[int]bool = make(map[int]bool, len(versions))
	var version int
	for _, version = range versions {
		applied[version] = true
	}
	return applied, nil
}

// applyOne runs a single migration and records it, both inside one transaction.
//
// Coupling the change to its record is the point: if the process dies between
// the two, neither happened, and the next boot retries cleanly. gorm's
// Transaction helper commits on a nil return and rolls back otherwise, and —
// unlike a hand-rolled Begin/Commit — it reports a failed commit instead of
// dropping it.
func applyOne(db *gorm.DB, version int, migrate MigrationFunc) error {
	var err error = db.Transaction(func(tx *gorm.DB) error {
		var migrateErr error = migrate(tx)
		if migrateErr != nil {
			return migrateErr
		}
		return tx.Create(&MigrationRecord{Version: version, AppliedAt: time.Now().UTC()}).Error
	})
	if err != nil {
		return errors.NewInternalError(
			fmt.Sprintf("migration v%d failed and was rolled back", version), err)
	}
	return nil
}

// validateContiguous refuses a catalogue whose versions are not exactly
// 1..len(m).
//
// The runner walks the integers from 1 upwards, so a gap would silently
// truncate the run at the hole and leave every later migration unapplied — and
// then, on the next boot, look up a version that is not there. Checking first
// turns that into one clear error, before anything has been written.
//
// An empty catalogue is valid: a project with no migrations yet is the normal
// starting state.
func validateContiguous(m map[int]MigrationFunc) error {
	if len(m) == 0 {
		return nil
	}

	var highest int
	var version int
	var migrate MigrationFunc
	for version, migrate = range m {
		if version < 1 {
			return errors.NewInternalError(
				fmt.Sprintf("migration versions start at 1, got %d", version), nil)
		}
		if migrate == nil {
			return errors.NewInternalError(
				fmt.Sprintf("migration v%d is nil", version), nil)
		}
		if version > highest {
			highest = version
		}
	}

	var missing []int
	for version = 1; version <= highest; version++ {
		if m[version] == nil {
			missing = append(missing, version)
		}
	}
	if len(missing) > 0 {
		return errors.NewInternalError(fmt.Sprintf(
			"migration versions must be contiguous from 1: %v missing, highest is %d — "+
				"renumber so that every version from 1 to %d exists",
			missing, highest, highest), nil)
	}

	return nil
}

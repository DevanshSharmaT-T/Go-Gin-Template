// File: internal/shared/database/module.go

package database

import (
	"context"
	"database/sql"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database/migrations"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// Module provides the database connection and hangs the boot sequence off fx's
// lifecycle.
//
// Include it once, from the entry point or the test harness. Everything else
// depends on *gorm.DB and gets it from here.
//
//	fx.New(
//		config.Module,
//		database.Module,
//		users.Module,
//	)
//
// The fx.Invoke is load-bearing. fx builds lazily: without something forcing
// construction, a graph nobody has asked for a *gorm.DB from would never
// connect and never migrate. This is the one place in the shared kernel that
// invokes rather than merely provides.
var Module = fx.Module("database",
	fx.Provide(Connect),
	fx.Invoke(register),
)

// Params collects what the boot sequence needs, including the two value groups
// through which modules register themselves.
//
// A module contributes to them from its own module.go — no central list to
// edit, and no merge conflict when two branches add a model:
//
//	fx.Provide(
//		fx.Annotate(
//			func() any { return &domain.User{} },
//			fx.ResultTags(`group:"models"`),
//		),
//		fx.Annotate(
//			seeds.NewUserSeeder,                     // must return database.SeedFunc
//			fx.ResultTags(`group:"seeders"`),
//		),
//	)
//
// Both stanzas fail silently when they are wrong, in the same way: fx cannot
// tell an empty value group from one nobody meant to fill. Omit the models
// entry and the table is never created — the application boots and fails on the
// first query. Declare a seeder as a bare func(*gorm.DB) error instead of a
// database.SeedFunc and it lands in a different group slot, where nothing
// collects it, and it simply never runs.
type Params struct {
	fx.In

	Lifecycle fx.Lifecycle
	DB        *gorm.DB

	Models  []any      `group:"models"`
	Seeders []SeedFunc `group:"seeders"`
}

// register attaches the start and stop hooks.
func register(p Params) {
	p.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return start(ctx, p)
		},
		OnStop: func(ctx context.Context) error {
			return stop(ctx, p.DB)
		},
	})
}

// start is steps 3 to 6 of the boot sequence in docs/ARCHITECTURE.md, and the
// order is load-bearing.
//
// Ping first, so an unreachable database is one clear error rather than a
// confusing failure inside a migration. The extension next, because the uuid
// defaults in the models depend on it. Then the two schema mechanisms, in this
// order: AutoMigrate creates and widens tables from the registered models'
// current tags, and the versioned migrations then apply the things AutoMigrate
// cannot express — drops, renames, backfills, type and constraint changes. A
// migration that alters a column can only run once the column exists.
//
// Seeding is last, because it writes rows into the schema the previous two
// steps just finished shaping.
//
// Every step takes ctx. fx cancels it when the start timeout expires, so a
// migration waiting on a lock held by another connection fails the boot instead
// of hanging it.
func start(ctx context.Context, p Params) error {
	var err error

	err = Ping(ctx, p.DB)
	if err != nil {
		return err
	}

	err = EnsureExtensions(ctx, p.DB)
	if err != nil {
		return err
	}

	err = AutoMigrate(ctx, p.DB, p.Models)
	if err != nil {
		return err
	}

	err = migrations.Run(ctx, p.DB)
	if err != nil {
		return err
	}

	return RunSeeders(ctx, p.DB, p.Seeders)
}

// stop closes the pool, releasing the server-side connections rather than
// leaving them for PostgreSQL to time out.
func stop(ctx context.Context, db *gorm.DB) error {
	var sqlDB *sql.DB
	var err error
	sqlDB, err = db.DB()
	if err != nil {
		return errors.NewInternalError("could not reach the underlying sql.DB to close it", err)
	}

	logger.FromContext(ctx).Debug().Msg("closing the database connection pool")

	err = sqlDB.Close()
	if err != nil {
		return errors.NewInternalError("failed to close the database connection pool", err)
	}
	return nil
}

// AutoMigrate creates or widens the tables for every model in the
// `group:"models"` value group.
//
// This is the mechanism behind "add an entity, get a table". It is additive
// only: GORM creates missing tables, adds missing columns and indexes, and
// widens types where it safely can. It will not drop a column, rename one,
// narrow a type or add a constraint — those need a numbered migration, which is
// why the two run one after the other.
func AutoMigrate(ctx context.Context, db *gorm.DB, models []any) error {
	if len(models) == 0 {
		logger.FromContext(ctx).Debug().Msg("no models registered for auto-migration")
		return nil
	}

	var err error = db.WithContext(ctx).AutoMigrate(models...)
	if err != nil {
		return errors.NewInternalError("failed to auto-migrate the registered models", err)
	}

	logger.FromContext(ctx).Info().Int("models", len(models)).Msg("auto-migrated registered models")
	return nil
}

// File: internal/shared/database/connector.go

// Package database owns the connection to PostgreSQL and everything that runs
// against it at boot: the pool, the query logger, schema migration, seeding and
// the translation of driver errors into AppErrors.
//
// Three boundaries are worth stating up front.
//
//   - This package and a module's infra/ are the only places that import gorm
//     and pgx. Everything above takes a *gorm.DB, or better, a repository
//     interface declared in domain/.
//
//   - No driver error leaves here unclassified. [TranslateError] turns a pgx
//     SQLSTATE or a GORM sentinel into an *errors.AppError, so a unique-index
//     violation becomes a CONFLICT rather than a 500 with the constraint name
//     in the response body.
//
//   - Everything that touches the database at boot takes a context.Context and
//     passes it on with db.WithContext. That is what lets fx's start timeout
//     actually cancel a migration that is blocked on a lock, instead of hanging
//     the process until someone notices.
package database

import (
	"database/sql"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// Connect opens the connection pool and configures it from cfg.
//
// It does not talk to the server. GORM would normally ping during Open, but a
// constructor has no context, so that ping could not honour a deadline and a
// dead database would hang startup rather than fail it. Automatic pinging is
// therefore disabled here and the real check happens in the OnStart hook, where
// [Ping] runs under fx's start timeout.
//
// Two GORM settings are deliberate:
//
//   - TranslateError makes the Postgres dialect map SQLSTATE codes onto GORM's
//     own sentinels (gorm.ErrDuplicatedKey and friends). Without it every
//     constraint violation arrives as an opaque *pgconn.PgError and
//     [TranslateError] has one fewer reliable signal to work from.
//   - NowFunc returns UTC. GORM's default is time.Now().Local(), which writes
//     whatever zone the server happens to run in into every timestamp column
//     and makes rows written by two hosts incomparable.
func Connect(cfg *config.Config) (*gorm.DB, error) {
	var db *gorm.DB
	var err error
	db, err = gorm.Open(postgres.Open(cfg.Database.URL), &gorm.Config{
		Logger:               newGormLogger(cfg.Database.LogLevel),
		TranslateError:       true,
		DisableAutomaticPing: true,
		NowFunc:              func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, errors.NewUnavailableError("failed to connect to database", err)
	}

	var sqlDB *sql.DB
	sqlDB, err = db.DB()
	if err != nil {
		return nil, errors.NewInternalError("could not reach the underlying sql.DB", err)
	}

	// Every one of these has a default of "unlimited", which is the wrong
	// default in front of a database that has a hard connection limit of its
	// own: under load the pool opens connections until PostgreSQL refuses them,
	// and the failure shows up as an outage rather than as queueing.
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.Database.ConnMaxIdleTime)

	return db, nil
}

// File: internal/shared/database/health.go

package database

import (
	"context"
	"database/sql"

	"gorm.io/gorm"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// Ping checks that the database is reachable and answering, within whatever
// deadline ctx carries.
//
// It is used twice: once in the OnStart hook, where it is the first thing that
// runs and turns an unreachable database into a startup failure with a clear
// message, and again by the readiness probe, where it is the difference between
// "the process is up" and "the process can serve a request".
//
// Ping is deliberately not a query. A SELECT would also exercise permissions
// and the schema, which sounds thorough until a probe starts failing for a
// reason that has nothing to do with connectivity.
func Ping(ctx context.Context, db *gorm.DB) error {
	var sqlDB *sql.DB
	var err error
	sqlDB, err = db.DB()
	if err != nil {
		return errors.NewInternalError("could not reach the underlying sql.DB", err)
	}

	err = sqlDB.PingContext(ctx)
	if err != nil {
		return errors.NewUnavailableError("failed to connect to database", err)
	}
	return nil
}

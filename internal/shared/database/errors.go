// File: internal/shared/database/errors.go

package database

import (
	"context"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// PostgreSQL SQLSTATE codes, from
// https://www.postgresql.org/docs/current/errcodes-appendix.html.
//
// Matching on the code is the whole point: it is a stable, documented contract,
// where the message that accompanies it is localised, version-dependent prose.
// Nothing in this file compares error *text*.
const (
	sqlStateStringTruncation    = "22001"
	sqlStateInvalidTextRepr     = "22P02"
	sqlStateNotNullViolation    = "23502"
	sqlStateForeignKeyViolation = "23503"
	sqlStateUniqueViolation     = "23505"
	sqlStateCheckViolation      = "23514"
	sqlStateSerializationFail   = "40001"
	sqlStateDeadlockDetected    = "40P01"
	sqlStateTooManyConnections  = "53300"
	sqlStateQueryCanceled       = "57014"
	sqlStateAdminShutdown       = "57P01"
	sqlStateCrashShutdown       = "57P02"
	sqlStateCannotConnectNow    = "57P03"

	// Class 08 is "connection exception" — every code in it means the
	// connection itself failed rather than the statement.
	sqlStateClassConnection = "08"
)

// TranslateError classifies a database error as an *errors.AppError.
//
// It is the seam between the driver and the rest of the application: a
// repository calls it on every error it gets back from GORM, and from that
// point on the failure has a classification, an HTTP status and a message that
// is safe to send. Nothing above the adapter should ever see a *pgconn.PgError.
//
// The rule about what reaches the client is the important one. The original
// error becomes the AppError's Cause, which is logged and never serialised, so
// the constraint name in "duplicate key value violates unique constraint
// \"users_email_key\"" stays in the log. Publishing it would hand out the table
// and column layout for free, and would confirm that a particular address is
// already registered. The caller gets CONFLICT and a generic sentence.
//
// An error that is already an *AppError is returned unchanged: a repository
// that has classified something more precisely than this function can — "that
// email is taken" rather than "the value already exists" — must win.
func TranslateError(err error) *errors.AppError {
	if err == nil {
		return nil
	}

	var appErr *errors.AppError
	if errors.As(err, &appErr) {
		return appErr
	}

	// GORM's own sentinels come first. With gorm.Config.TranslateError enabled
	// the Postgres dialect wraps the pgx error in one of these, so the sentinel
	// and the SQLSTATE below agree; checking the sentinel first keeps the two
	// paths from drifting apart.
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return errors.NewNotFoundError("the requested record does not exist", err)
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return errors.NewConflictError("that value is already taken", err)
	case errors.Is(err, gorm.ErrForeignKeyViolated):
		return errors.NewConflictError("a related record is missing or still in use", err)
	case errors.Is(err, gorm.ErrCheckConstraintViolated):
		return errors.NewValidationError("a value failed a database constraint", err)
	case errors.Is(err, context.DeadlineExceeded):
		return errors.NewTimeoutError("the database operation exceeded its deadline", err)
	case errors.Is(err, context.Canceled):
		return errors.NewTimeoutError("the database operation was cancelled", err)
	case errors.Is(err, syscall.ECONNREFUSED):
		return errors.NewUnavailableError("the database refused the connection", err)
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return translatePgError(pgErr.Code, err)
	}

	// Dial failures never reach the server, so they carry no SQLSTATE.
	var connErr *pgconn.ConnectError
	if errors.As(err, &connErr) {
		return errors.NewUnavailableError("could not reach the database", err)
	}

	// Anything unrecognised is ours until proven otherwise. The cause is kept
	// for the log; the caller gets the generic 500 message from Response.
	return errors.NewInternalError("database operation failed", err)
}

// translatePgError maps a SQLSTATE onto a classification.
//
// The grouping is about who has to act. A unique or foreign-key violation is a
// collision with state the caller can see and retry differently, so it is a
// CONFLICT. A not-null, check or format violation is input that should never
// have been accepted, so it is a VALIDATION error — and a reminder that the
// same rule belongs in the service layer, where it can name the field.
func translatePgError(code string, cause error) *errors.AppError {
	switch code {
	case sqlStateUniqueViolation:
		return errors.NewConflictError("that value is already taken", cause)
	case sqlStateForeignKeyViolation:
		return errors.NewConflictError("a related record is missing or still in use", cause)

	case sqlStateSerializationFail, sqlStateDeadlockDetected:
		// Both mean "you lost a concurrent update". They are retryable, which
		// is worth telling the caller rather than hiding behind a 500.
		return errors.NewConflictError("the record changed concurrently; retry the request", cause)

	case sqlStateNotNullViolation:
		return errors.NewValidationError("a required value is missing", cause)
	case sqlStateCheckViolation:
		return errors.NewValidationError("a value failed a database constraint", cause)
	case sqlStateStringTruncation, sqlStateInvalidTextRepr:
		return errors.NewValidationError("a value has the wrong format or is too long", cause)

	case sqlStateQueryCanceled:
		return errors.NewTimeoutError("the database cancelled the statement", cause)

	case sqlStateTooManyConnections, sqlStateAdminShutdown,
		sqlStateCrashShutdown, sqlStateCannotConnectNow:
		return errors.NewUnavailableError("the database is not accepting queries", cause)
	}

	if strings.HasPrefix(code, sqlStateClassConnection) {
		return errors.NewUnavailableError("the database connection failed", cause)
	}

	return errors.NewInternalError("database operation failed", cause)
}

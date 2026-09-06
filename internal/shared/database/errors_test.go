// File: internal/shared/database/errors_test.go

package database

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// duplicateKeyPgError is the shape pgx really returns for a unique-index
// violation, constraint name and all. Everything that must not reach a client
// is in here.
func duplicateKeyPgError() *pgconn.PgError {
	return &pgconn.PgError{
		Severity:       "ERROR",
		Code:           sqlStateUniqueViolation,
		Message:        `duplicate key value violates unique constraint "users_email_key"`,
		Detail:         `Key (email)=(someone@example.com) already exists.`,
		ConstraintName: "users_email_key",
		TableName:      "users",
	}
}

// The failure messages below deliberately print only the resulting
// classification, never the input error: one of the cases is a
// *pgconn.ConnectError, whose Error method dereferences a field that cannot be
// populated from outside the package.
func TestTranslateError_ClassifiesEveryBranch(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want apperrors.ErrorType
	}{
		{"record not found", gorm.ErrRecordNotFound, apperrors.TypeNotFound},
		{"wrapped record not found", fmt.Errorf("find user: %w", gorm.ErrRecordNotFound), apperrors.TypeNotFound},

		{"gorm duplicated key", gorm.ErrDuplicatedKey, apperrors.TypeConflict},
		{"gorm foreign key", gorm.ErrForeignKeyViolated, apperrors.TypeConflict},
		{"gorm check constraint", gorm.ErrCheckConstraintViolated, apperrors.TypeValidation},

		// What the Postgres dialect actually hands back with
		// gorm.Config.TranslateError enabled: the sentinel and the driver
		// error, wrapped together.
		{
			"dialect-translated unique violation",
			fmt.Errorf("%w: %w", gorm.ErrDuplicatedKey, duplicateKeyPgError()),
			apperrors.TypeConflict,
		},

		{"unique violation", duplicateKeyPgError(), apperrors.TypeConflict},
		{"foreign key violation", &pgconn.PgError{Code: sqlStateForeignKeyViolation}, apperrors.TypeConflict},
		{"serialization failure", &pgconn.PgError{Code: sqlStateSerializationFail}, apperrors.TypeConflict},
		{"deadlock detected", &pgconn.PgError{Code: sqlStateDeadlockDetected}, apperrors.TypeConflict},

		{"not-null violation", &pgconn.PgError{Code: sqlStateNotNullViolation}, apperrors.TypeValidation},
		{"check violation", &pgconn.PgError{Code: sqlStateCheckViolation}, apperrors.TypeValidation},
		{"string truncation", &pgconn.PgError{Code: sqlStateStringTruncation}, apperrors.TypeValidation},
		{"invalid text representation", &pgconn.PgError{Code: sqlStateInvalidTextRepr}, apperrors.TypeValidation},

		{"deadline exceeded", context.DeadlineExceeded, apperrors.TypeTimeout},
		{"cancelled", context.Canceled, apperrors.TypeTimeout},
		{"query cancelled by the server", &pgconn.PgError{Code: sqlStateQueryCanceled}, apperrors.TypeTimeout},

		{"connection refused", fmt.Errorf("dial tcp: %w", syscall.ECONNREFUSED), apperrors.TypeUnavailable},
		{"too many connections", &pgconn.PgError{Code: sqlStateTooManyConnections}, apperrors.TypeUnavailable},
		{"admin shutdown", &pgconn.PgError{Code: sqlStateAdminShutdown}, apperrors.TypeUnavailable},
		{"cannot connect now", &pgconn.PgError{Code: sqlStateCannotConnectNow}, apperrors.TypeUnavailable},
		{"connection exception class", &pgconn.PgError{Code: "08006"}, apperrors.TypeUnavailable},
		{"dial failure with no SQLSTATE", &pgconn.ConnectError{Config: &pgconn.Config{}}, apperrors.TypeUnavailable},

		{"unrecognised driver error", stderrors.New("something went sideways"), apperrors.TypeInternal},
		{"unmapped SQLSTATE", &pgconn.PgError{Code: "XX000"}, apperrors.TypeInternal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TranslateError(tc.err)
			if got == nil {
				t.Fatalf("want %s, got nil", tc.want)
			}
			if got.Type != tc.want {
				t.Fatalf("want %s, got %s", tc.want, got.Type)
			}
		})
	}
}

func TestTranslateError_NilIsNil(t *testing.T) {
	if got := TranslateError(nil); got != nil {
		t.Fatalf("want nil for a nil error, got %s", got.Type)
	}
}

// A repository that has already classified a failure more precisely than this
// function can must not have that classification overwritten on the way out.
func TestTranslateError_KeepsAnExistingClassification(t *testing.T) {
	original := apperrors.NewForbiddenError("row-level security refused the update", nil)

	got := TranslateError(fmt.Errorf("update user: %w", original))

	if got != original {
		t.Fatalf("want the original AppError back, got %v", got)
	}
}

// The regression that matters: a unique-index violation names the constraint,
// the table and the offending value, and none of that may reach a client.
func TestTranslateError_DoesNotLeakConstraintDetailToTheClient(t *testing.T) {
	got := TranslateError(duplicateKeyPgError())

	if got.ToHTTPStatus() != http.StatusConflict {
		t.Fatalf("want 409, got %d", got.ToHTTPStatus())
	}

	response := got.Response()
	for _, secret := range []string{"users_email_key", "someone@example.com", "users", "23505"} {
		if strings.Contains(response.Error, secret) {
			t.Fatalf("response leaked %q: %q", secret, response.Error)
		}
	}

	// The detail is still reachable for the log, which is the whole point of
	// keeping the cause.
	if !strings.Contains(got.Error(), "users_email_key") {
		t.Fatalf("the cause should still name the constraint for the log, got %q", got.Error())
	}
}

// A 5xx classification must not describe the failure to the caller either, even
// though its message is written for an operator.
func TestTranslateError_RedactsInternalFailures(t *testing.T) {
	got := TranslateError(stderrors.New("pq: password authentication failed for user \"app\""))

	if got.Type != apperrors.TypeInternal {
		t.Fatalf("want INTERNAL, got %s", got.Type)
	}
	if strings.Contains(got.Response().Error, "password") {
		t.Fatalf("the response leaked the driver message: %q", got.Response().Error)
	}
}

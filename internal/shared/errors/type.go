// File: internal/shared/errors/type.go

package errors

import "net/http"

// ErrorType is the classification carried by every AppError. It is the only
// thing that decides an HTTP status, so the mapping below is the single place
// the application's "failure vocabulary" is defined.
//
// The string values are part of the public API: they are serialised into the
// `type` field of every error response, and clients branch on them.
type ErrorType string

const (
	// 400 — the request itself is wrong.
	TypeValidation ErrorType = "VALIDATION"
	TypeBadRequest ErrorType = "BAD_REQUEST"

	// 401 — the caller is not authenticated, or the token is unusable.
	TypeUnauthorized       ErrorType = "UNAUTHORIZED"
	TypeInvalidCredentials ErrorType = "INVALID_CREDENTIALS"
	TypeTokenInvalid       ErrorType = "TOKEN_INVALID"
	TypeTokenExpired       ErrorType = "TOKEN_EXPIRED"
	// TypeTokenStale is returned when a token's permission version no longer
	// matches the role's current version — i.e. permissions changed under it.
	TypeTokenStale ErrorType = "TOKEN_STALE"

	// 403 — authenticated, but not allowed.
	TypeForbidden     ErrorType = "FORBIDDEN"
	TypeUserSuspended ErrorType = "USER_SUSPENDED"

	// 404 / 405 / 409 / 410 — addressing and state conflicts.
	TypeNotFound         ErrorType = "NOT_FOUND"
	TypeMethodNotAllowed ErrorType = "METHOD_NOT_ALLOWED"
	TypeConflict         ErrorType = "CONFLICT"
	TypeGone             ErrorType = "GONE"

	// 413 / 415 / 422 / 429 — the request is well-formed but unacceptable.
	TypePayloadTooLarge ErrorType = "PAYLOAD_TOO_LARGE"
	TypeUnsupportedType ErrorType = "UNSUPPORTED_MEDIA_TYPE"
	TypeUnprocessable   ErrorType = "UNPROCESSABLE"
	TypeRateLimited     ErrorType = "RATE_LIMITED"

	// 5xx — the failure is ours. Messages for these are never returned to the
	// caller verbatim; see AppError.Response.
	TypeInternal       ErrorType = "INTERNAL"
	TypeNotImplemented ErrorType = "NOT_IMPLEMENTED"
	TypeDependency     ErrorType = "DEPENDENCY"
	TypeUnavailable    ErrorType = "UNAVAILABLE"
	TypeTimeout        ErrorType = "TIMEOUT"
)

// statusByType is the one and only type-to-status mapping. Changing the status
// for a classification is a one-line change that applies application-wide.
var statusByType map[ErrorType]int = map[ErrorType]int{
	TypeValidation:         http.StatusBadRequest,
	TypeBadRequest:         http.StatusBadRequest,
	TypeUnauthorized:       http.StatusUnauthorized,
	TypeInvalidCredentials: http.StatusUnauthorized,
	TypeTokenInvalid:       http.StatusUnauthorized,
	TypeTokenExpired:       http.StatusUnauthorized,
	TypeTokenStale:         http.StatusUnauthorized,
	TypeForbidden:          http.StatusForbidden,
	TypeUserSuspended:      http.StatusForbidden,
	TypeNotFound:           http.StatusNotFound,
	TypeMethodNotAllowed:   http.StatusMethodNotAllowed,
	TypeConflict:           http.StatusConflict,
	TypeGone:               http.StatusGone,
	TypePayloadTooLarge:    http.StatusRequestEntityTooLarge,
	TypeUnsupportedType:    http.StatusUnsupportedMediaType,
	TypeUnprocessable:      http.StatusUnprocessableEntity,
	TypeRateLimited:        http.StatusTooManyRequests,
	TypeInternal:           http.StatusInternalServerError,
	TypeNotImplemented:     http.StatusNotImplemented,
	TypeDependency:         http.StatusBadGateway,
	TypeUnavailable:        http.StatusServiceUnavailable,
	TypeTimeout:            http.StatusGatewayTimeout,
}

// safeMessageByType supplies the text returned to the caller for server-side
// failures. The operator-facing message is kept on the AppError for the log.
var safeMessageByType map[ErrorType]string = map[ErrorType]string{
	TypeInternal:       "internal server error",
	TypeNotImplemented: "not implemented",
	TypeDependency:     "an upstream dependency failed",
	TypeUnavailable:    "service temporarily unavailable",
	TypeTimeout:        "the request timed out",
}

// HTTPStatus maps the classification onto an HTTP status code. An unknown type
// is treated as a server fault: a classification we forgot to map is a bug on
// our side, not the caller's.
func (t ErrorType) HTTPStatus() int {
	var status int
	var ok bool
	status, ok = statusByType[t]
	if !ok {
		return http.StatusInternalServerError
	}
	return status
}

// String makes ErrorType printable in log lines and test failures.
func (t ErrorType) String() string {
	return string(t)
}

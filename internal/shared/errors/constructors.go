// File: internal/shared/errors/constructors.go

package errors

// One constructor per classification. Every one takes the same pair —
// (message, cause) — so switching a classification is a one-word edit and
// never a rewrite of the call site. Pass nil for cause when there is no
// underlying error to keep.
//
// Classify at the point of origin: the layer that knows *why* something failed
// is the layer that must say so.

// --- 400 ---------------------------------------------------------------------

// NewValidationError reports input that failed a rule — a missing field, a
// malformed email, a password below the minimum length. Attach per-field
// reasons with WithDetails.
func NewValidationError(message string, cause error) *AppError {
	return New(TypeValidation, message, cause)
}

// NewBadRequestError reports a request that is malformed at a level below
// validation: undecodable JSON, an unparseable UUID in the path.
func NewBadRequestError(message string, cause error) *AppError {
	return New(TypeBadRequest, message, cause)
}

// --- 401 ---------------------------------------------------------------------

// NewUnauthorizedError reports a missing or unusable credential.
func NewUnauthorizedError(message string, cause error) *AppError {
	return New(TypeUnauthorized, message, cause)
}

// NewInvalidCredentialsError reports a failed login.
//
// Use one message for every failure mode — unknown user, wrong password,
// disabled account. Distinguishing them tells an attacker which half of the
// pair was correct, and turns the endpoint into a user-enumeration oracle.
func NewInvalidCredentialsError(message string, cause error) *AppError {
	return New(TypeInvalidCredentials, message, cause)
}

// NewTokenInvalidError reports a token that failed signature, issuer or
// structural verification.
func NewTokenInvalidError(message string, cause error) *AppError {
	return New(TypeTokenInvalid, message, cause)
}

// NewTokenExpiredError reports a well-formed token that is past its expiry.
func NewTokenExpiredError(message string, cause error) *AppError {
	return New(TypeTokenExpired, message, cause)
}

// NewTokenStaleError reports a token whose permission version no longer matches
// the role's current version. It is the revocation path: permissions changed,
// so every token issued before the change is refused. Clients should treat it
// as "log in again", not as an error to retry.
func NewTokenStaleError(message string, cause error) *AppError {
	return New(TypeTokenStale, message, cause)
}

// --- 403 ---------------------------------------------------------------------

// NewForbiddenError reports an authenticated caller without the required
// permission or hierarchy level.
func NewForbiddenError(message string, cause error) *AppError {
	return New(TypeForbidden, message, cause)
}

// NewUserSuspendedError reports a caller whose account has been suspended. It
// is distinct from FORBIDDEN so a client can tell "you may not do this" from
// "your account is disabled".
func NewUserSuspendedError(message string, cause error) *AppError {
	return New(TypeUserSuspended, message, cause)
}

// --- 404 / 405 / 409 / 410 ---------------------------------------------------

// NewNotFoundError reports an entity that does not exist. Repositories return
// it in place of the driver's own not-found error.
func NewNotFoundError(message string, cause error) *AppError {
	return New(TypeNotFound, message, cause)
}

// NewMethodNotAllowedError reports a known path used with the wrong verb.
func NewMethodNotAllowedError(message string, cause error) *AppError {
	return New(TypeMethodNotAllowed, message, cause)
}

// NewConflictError reports a collision with existing state — a duplicate email,
// a unique-index violation, a concurrent update that lost.
func NewConflictError(message string, cause error) *AppError {
	return New(TypeConflict, message, cause)
}

// NewGoneError reports something that existed and deliberately no longer does,
// such as a consumed single-use token.
func NewGoneError(message string, cause error) *AppError {
	return New(TypeGone, message, cause)
}

// --- 413 / 415 / 422 / 429 ---------------------------------------------------

// NewPayloadTooLargeError reports a body over the configured limit.
func NewPayloadTooLargeError(message string, cause error) *AppError {
	return New(TypePayloadTooLarge, message, cause)
}

// NewUnsupportedTypeError reports an unacceptable Content-Type.
func NewUnsupportedTypeError(message string, cause error) *AppError {
	return New(TypeUnsupportedType, message, cause)
}

// NewUnprocessableError reports a request that is syntactically valid and
// individually well-formed, but cannot be acted on — for example a state
// transition the entity does not allow from where it is now.
func NewUnprocessableError(message string, cause error) *AppError {
	return New(TypeUnprocessable, message, cause)
}

// NewRateLimitedError reports a caller over its rate limit.
func NewRateLimitedError(message string, cause error) *AppError {
	return New(TypeRateLimited, message, cause)
}

// --- 5xx ---------------------------------------------------------------------
//
// For every constructor below, the message is written for the log. Response
// substitutes a generic string, so be as specific here as the log needs.

// NewInternalError reports a failure with no better classification. Always pass
// the cause: it is the only record of what actually happened.
func NewInternalError(message string, cause error) *AppError {
	return New(TypeInternal, message, cause)
}

// NewNotImplementedError reports a route or branch that exists but is not built
// yet.
func NewNotImplementedError(message string, cause error) *AppError {
	return New(TypeNotImplemented, message, cause)
}

// NewDependencyError reports a failure in something we call out to — the mail
// server, an OAuth provider, any upstream API. It is separate from INTERNAL so
// that "we are broken" and "something we depend on is broken" can be alerted on
// differently.
func NewDependencyError(message string, cause error) *AppError {
	return New(TypeDependency, message, cause)
}

// NewUnavailableError reports a dependency that is reachable but not ready —
// typically the database during startup or a failover.
func NewUnavailableError(message string, cause error) *AppError {
	return New(TypeUnavailable, message, cause)
}

// NewTimeoutError reports work that exceeded its deadline.
func NewTimeoutError(message string, cause error) *AppError {
	return New(TypeTimeout, message, cause)
}

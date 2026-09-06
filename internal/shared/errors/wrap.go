// File: internal/shared/errors/wrap.go

package errors

import stderrors "errors"

// This package shadows the standard library's "errors", so a file that imports
// it cannot conveniently import the standard one too. These re-exports mean it
// does not have to: errors.As, errors.Is, errors.Join and errors.Unwrap behave
// exactly as they do in the standard library.

// As is stderrors.As.
func As(err error, target any) bool {
	return stderrors.As(err, target)
}

// Is is stderrors.Is.
func Is(err error, target error) bool {
	return stderrors.Is(err, target)
}

// Join is stderrors.Join.
func Join(errs ...error) error {
	return stderrors.Join(errs...)
}

// Unwrap is stderrors.Unwrap.
func Unwrap(err error) error {
	return stderrors.Unwrap(err)
}

// AsAppError reports whether err is, or wraps, an *AppError and returns it.
//
//	var appErr *errors.AppError
//	var ok bool
//	appErr, ok = errors.AsAppError(err)
func AsAppError(err error) (*AppError, bool) {
	if err == nil {
		return nil, false
	}
	var appErr *AppError
	if stderrors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// From always returns an *AppError for a non-nil error, classifying anything
// unrecognised as INTERNAL with the original kept as the cause.
//
// This is what the transport layer's handleError uses: an unclassified error
// reaching it is a bug — some layer failed to classify — and the safe reading
// of a bug is that it is our fault, not the caller's.
//
// From(nil) returns nil, so it is safe to call unconditionally.
func From(err error) *AppError {
	if err == nil {
		return nil
	}
	var appErr *AppError
	if stderrors.As(err, &appErr) {
		return appErr
	}
	return NewInternalError("unhandled error", err)
}

// IsType reports whether err is, or wraps, an AppError of the given
// classification. Prefer it over comparing messages in tests and callers.
func IsType(err error, errType ErrorType) bool {
	var appErr *AppError
	var ok bool
	appErr, ok = AsAppError(err)
	if !ok {
		return false
	}
	return appErr.Type == errType
}

// File: internal/shared/errors/errors.go

// Package errors defines AppError, the single error type that is allowed to
// cross a layer boundary in this application.
//
// The contract is short:
//
//   - Whichever layer *knows* what went wrong classifies the failure at the
//     point of origin — the repository turns a driver error into NOT_FOUND or
//     CONFLICT, the service turns a broken rule into VALIDATION or FORBIDDEN.
//   - Every layer above simply propagates.
//   - The transport layer never chooses a status code by hand. It asks the
//     error: appErr.ToHTTPStatus().
//
// Three properties fall out of that, and none of them survives handlers that
// write their own c.JSON(500, gin.H{"error": err.Error()}):
//
//  1. The status mapping is defined once, in type.go.
//  2. Client mistakes (4xx) are distinguishable from server faults (5xx), so
//     error tracking can report the second and ignore the first.
//  3. Driver text never reaches a client. The cause is kept for the log; the
//     caller gets the classification.
//
// This package deliberately shadows the standard library's "errors". It
// re-exports As, Is, Join and Unwrap (see wrap.go) so that a file importing it
// does not need both.
package errors

import (
	stderrors "errors"
	"fmt"
)

// AppError is a classified application error.
//
// Message is written for whoever debugs the failure. For 4xx it is also what
// the caller sees; for 5xx it stays server-side and Response substitutes a
// generic string, because operator-facing text routinely names internal
// components.
type AppError struct {
	// Type is the classification. It decides the HTTP status.
	Type ErrorType

	// Message describes the failure. Keep it specific and free of secrets.
	Message string

	// Cause is the underlying error, if any. It is logged, never serialised.
	Cause error

	// Details carries field-level information for 4xx responses — typically
	// field name to reason, from request validation. Dropped for 5xx.
	Details map[string]string

	// RequestID correlates the response with the server log. It is normally
	// attached by the transport layer via WithRequestID.
	RequestID string
}

// ErrorResponse is the wire format. It is the only shape a client ever sees:
//
//	{"type":"NOT_FOUND","error":"user not found","request_id":"01H..."}
type ErrorResponse struct {
	Type      ErrorType         `json:"type"`
	Error     string            `json:"error"`
	RequestID string            `json:"request_id,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
}

// New builds an AppError directly. Prefer the named constructors in
// constructors.go; this exists for the rare classification built at runtime.
func New(errType ErrorType, message string, cause error) *AppError {
	return &AppError{
		Type:    errType,
		Message: message,
		Cause:   cause,
	}
}

// Error implements the error interface. This string is for logs and test
// output — it includes the cause and must never be written to a response.
func (e *AppError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// Unwrap exposes the cause to errors.Is and errors.As, so a caller can still
// test for a sentinel underneath the classification.
func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ToHTTPStatus is what handlers call instead of writing status-code logic.
func (e *AppError) ToHTTPStatus() int {
	if e == nil {
		return TypeInternal.HTTPStatus()
	}
	return e.Type.HTTPStatus()
}

// IsServerError reports whether this failure is ours (status >= 500). It is the
// signal used to decide what gets reported to error tracking: reporting 4xx as
// well drowns real faults in ordinary client mistakes.
func (e *AppError) IsServerError() bool {
	return e.ToHTTPStatus() >= 500
}

// Response builds the client-facing payload.
//
// For server-side failures the caller-supplied message is replaced with a
// generic one and Details are dropped. That asymmetry is the point: the
// specific message and the cause stay in the log, keyed by RequestID.
func (e *AppError) Response() *ErrorResponse {
	if e == nil {
		return &ErrorResponse{Type: TypeInternal, Error: safeMessageByType[TypeInternal]}
	}

	if e.IsServerError() {
		var safe string
		var ok bool
		safe, ok = safeMessageByType[e.Type]
		if !ok {
			safe = safeMessageByType[TypeInternal]
		}
		return &ErrorResponse{
			Type:      e.Type,
			Error:     safe,
			RequestID: e.RequestID,
		}
	}

	return &ErrorResponse{
		Type:      e.Type,
		Error:     e.Message,
		RequestID: e.RequestID,
		Details:   e.Details,
	}
}

// WithRequestID returns a copy carrying the request ID. It copies rather than
// mutating so that a shared sentinel AppError cannot be rewritten by whichever
// request happened to touch it last.
func (e *AppError) WithRequestID(requestID string) *AppError {
	if e == nil {
		return nil
	}
	var clone AppError = *e
	clone.RequestID = requestID
	return &clone
}

// WithDetails returns a copy carrying field-level detail. Details are only
// serialised for 4xx responses.
func (e *AppError) WithDetails(details map[string]string) *AppError {
	if e == nil {
		return nil
	}
	var clone AppError = *e
	clone.Details = details
	return &clone
}

// WithDetail returns a copy with a single field-level entry added.
func (e *AppError) WithDetail(field string, reason string) *AppError {
	if e == nil {
		return nil
	}
	var clone AppError = *e
	clone.Details = make(map[string]string, len(e.Details)+1)
	var k string
	var v string
	for k, v = range e.Details {
		clone.Details[k] = v
	}
	clone.Details[field] = reason
	return &clone
}

// Is lets errors.Is match two AppErrors on classification alone, so a test or
// a caller can ask "is this a NOT_FOUND?" without depending on the message.
func (e *AppError) Is(target error) bool {
	var other *AppError
	if !stderrors.As(target, &other) {
		return false
	}
	return e.Type == other.Type
}

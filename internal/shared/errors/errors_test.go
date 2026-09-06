// File: internal/shared/errors/errors_test.go

package errors

import (
	"encoding/json"
	stderrors "errors"
	"net/http"
	"testing"
)

func TestErrorType_HTTPStatus_MapsEveryDeclaredType(t *testing.T) {
	// Guards against adding a constructor without adding its status: an
	// unmapped type would silently become a 500 in production.
	cases := map[ErrorType]int{
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

	if len(cases) != len(statusByType) {
		t.Fatalf("statusByType has %d entries, test covers %d — a type was added without a test",
			len(statusByType), len(cases))
	}

	for errType, want := range cases {
		if got := errType.HTTPStatus(); got != want {
			t.Errorf("%s: want status %d, got %d", errType, want, got)
		}
	}
}

func TestErrorType_HTTPStatus_UnknownTypeIsServerFault(t *testing.T) {
	// A classification we forgot to map is our bug, not the caller's.
	if got := ErrorType("NOT_A_REAL_TYPE").HTTPStatus(); got != http.StatusInternalServerError {
		t.Errorf("want 500 for an unmapped type, got %d", got)
	}
}

func TestAppError_Response_RedactsServerErrorMessage(t *testing.T) {
	// The operator-facing message routinely names internal components. It must
	// stay in the log and out of the response.
	cause := stderrors.New(`pq: duplicate key value violates unique constraint "users_email_key"`)
	appErr := NewInternalError("failed to insert into users table", cause)

	resp := appErr.Response()

	if resp.Error == "failed to insert into users table" {
		t.Error("the operator-facing message leaked into the response")
	}
	if resp.Error != safeMessageByType[TypeInternal] {
		t.Errorf("want the generic message %q, got %q", safeMessageByType[TypeInternal], resp.Error)
	}
	if resp.Type != TypeInternal {
		t.Errorf("the classification must survive redaction, got %q", resp.Type)
	}

	// The cause must still be reachable for the log.
	if !stderrors.Is(appErr, cause) {
		t.Error("the cause was lost; it is the only record of what happened")
	}
	if got := appErr.Error(); got == resp.Error {
		t.Error("Error() should carry the detailed text, Response() the generic one")
	}
}

func TestAppError_Response_DropsDetailsForServerErrors(t *testing.T) {
	appErr := NewDependencyError("smtp handshake failed", nil).
		WithDetails(map[string]string{"host": "smtp.internal.example", "port": "587"})

	if resp := appErr.Response(); resp.Details != nil {
		t.Errorf("details must not be serialised for 5xx, got %v", resp.Details)
	}
}

func TestAppError_Response_KeepsClientErrorMessageAndDetails(t *testing.T) {
	appErr := NewValidationError("request body is invalid", nil).
		WithDetail("email", "must be a valid email address")

	resp := appErr.Response()

	if resp.Error != "request body is invalid" {
		t.Errorf("4xx messages are for the caller, got %q", resp.Error)
	}
	if resp.Details["email"] != "must be a valid email address" {
		t.Errorf("want field detail preserved, got %v", resp.Details)
	}
}

func TestAppError_Response_WireFormat(t *testing.T) {
	// The documented contract in docs/ARCHITECTURE.md.
	appErr := NewNotFoundError("user not found", nil).WithRequestID("01H")

	encoded, err := json.Marshal(appErr.Response())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	want := `{"type":"NOT_FOUND","error":"user not found","request_id":"01H"}`
	if string(encoded) != want {
		t.Errorf("wire format changed:\n want %s\n  got %s", want, encoded)
	}
}

func TestAppError_Response_OmitsEmptyRequestID(t *testing.T) {
	encoded, err := json.Marshal(NewNotFoundError("nope", nil).Response())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"type":"NOT_FOUND","error":"nope"}`; string(encoded) != want {
		t.Errorf("want %s, got %s", want, encoded)
	}
}

func TestAppError_WithRequestID_DoesNotMutateReceiver(t *testing.T) {
	// Constructors are often package-level sentinels; mutating in place would
	// let one request's ID show up in another's response.
	original := NewForbiddenError("nope", nil)

	first := original.WithRequestID("req-1")
	second := original.WithRequestID("req-2")

	if original.RequestID != "" {
		t.Errorf("receiver was mutated, RequestID = %q", original.RequestID)
	}
	if first.RequestID != "req-1" || second.RequestID != "req-2" {
		t.Errorf("copies got crossed: %q and %q", first.RequestID, second.RequestID)
	}
}

func TestAppError_WithDetail_DoesNotMutateReceiver(t *testing.T) {
	original := NewValidationError("invalid", nil).WithDetail("a", "1")
	extended := original.WithDetail("b", "2")

	if _, present := original.Details["b"]; present {
		t.Error("WithDetail mutated the receiver's map")
	}
	if extended.Details["a"] != "1" || extended.Details["b"] != "2" {
		t.Errorf("want both entries on the copy, got %v", extended.Details)
	}
}

func TestAppError_IsServerError(t *testing.T) {
	if NewValidationError("x", nil).IsServerError() {
		t.Error("400 must not be reported as a server fault")
	}
	if !NewInternalError("x", nil).IsServerError() {
		t.Error("500 must be reported as a server fault")
	}
	if !NewDependencyError("x", nil).IsServerError() {
		t.Error("502 must be reported as a server fault")
	}
}

func TestFrom_ClassifiesUnknownErrorsAsInternal(t *testing.T) {
	// An unclassified error reaching the transport layer means some layer
	// failed to classify — a bug on our side, so 500 is the honest answer.
	plain := stderrors.New("something from a library")

	converted := From(plain)

	if converted.Type != TypeInternal {
		t.Errorf("want INTERNAL, got %s", converted.Type)
	}
	if !stderrors.Is(converted, plain) {
		t.Error("the original error must remain reachable as the cause")
	}
}

func TestFrom_PassesThroughExistingAppErrors(t *testing.T) {
	original := NewConflictError("email already registered", nil)

	if converted := From(original); converted != original {
		t.Error("an already-classified error must not be re-wrapped")
	}
}

func TestFrom_NilIsNil(t *testing.T) {
	if From(nil) != nil {
		t.Error("From(nil) must be nil so callers can call it unconditionally")
	}
}

func TestAs_FindsAppErrorThroughWrapping(t *testing.T) {
	// The handleError pattern in docs/ARCHITECTURE.md relies on this.
	wrapped := stderrors.Join(stderrors.New("outer"), NewNotFoundError("user not found", nil))

	var appErr *AppError
	if !As(wrapped, &appErr) {
		t.Fatal("As failed to find the AppError through a wrapper")
	}
	if appErr.ToHTTPStatus() != http.StatusNotFound {
		t.Errorf("want 404, got %d", appErr.ToHTTPStatus())
	}
}

func TestIsType_MatchesClassificationNotMessage(t *testing.T) {
	err := NewConflictError("email already registered", nil)

	if !IsType(err, TypeConflict) {
		t.Error("want a CONFLICT match")
	}
	if IsType(err, TypeNotFound) {
		t.Error("must not match a different classification")
	}
	if IsType(stderrors.New("plain"), TypeConflict) {
		t.Error("a plain error must not match any classification")
	}
}

func TestAppError_Is_ComparesOnTypeAlone(t *testing.T) {
	// Lets a test assert the contract without depending on wording.
	if !stderrors.Is(NewNotFoundError("user not found", nil), NewNotFoundError("anything", nil)) {
		t.Error("two NOT_FOUND errors should match regardless of message")
	}
	if stderrors.Is(NewNotFoundError("x", nil), NewConflictError("x", nil)) {
		t.Error("different classifications must not match")
	}
}

func TestAppError_NilReceiverIsSafe(t *testing.T) {
	// Handlers call these on values that may be nil after a failed type
	// assertion; panicking there would turn a handled error into a 500 panic.
	var appErr *AppError

	if got := appErr.Error(); got != "<nil>" {
		t.Errorf("want \"<nil>\", got %q", got)
	}
	if got := appErr.ToHTTPStatus(); got != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", got)
	}
	if resp := appErr.Response(); resp == nil || resp.Type != TypeInternal {
		t.Error("want a usable INTERNAL response from a nil receiver")
	}
	if appErr.WithRequestID("x") != nil || appErr.WithDetail("a", "b") != nil {
		t.Error("copy helpers on a nil receiver should stay nil")
	}
}

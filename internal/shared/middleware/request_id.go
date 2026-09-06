// File: internal/shared/middleware/request_id.go

package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Every middleware in this package has a named type rather than being a bare
// gin.HandlerFunc, and that is a wiring requirement rather than a style
// preference. fx keys providers by type, so a second provider returning
// gin.HandlerFunc collides with the JWT middleware and the application fails to
// start with a duplicate-type error.

// RequestIDMiddleware attaches a correlation ID to every request.
type RequestIDMiddleware gin.HandlerFunc

// RequestIDHeader is the header the ID is read from and written back on.
const RequestIDHeader = "X-Request-ID"

// ContextRequestID is the Gin context key holding the ID.
const ContextRequestID = "request_id"

// maxRequestIDLength bounds an inbound ID.
const maxRequestIDLength = 64

// NewRequestIDMiddleware builds the middleware.
//
// An inbound X-Request-ID is honoured, so a trace started by a gateway or by a
// calling service carries through into these logs. **It is validated first, and
// that is not optional.** The header is attacker-controlled and its value goes
// straight into every log line for the request:
//
//   - A newline in it forges log entries. Anything reading those logs — a
//     human, or a parser that treats one line as one event — sees whatever the
//     caller decided to write.
//   - An unbounded value is a cheap way to write megabytes per request into
//     log storage.
//
// So the value must be short and drawn from a small alphabet. Anything else is
// discarded and replaced with a generated ID rather than sanitised in place:
// truncating or stripping characters silently maps two different traces onto
// one identifier.
func NewRequestIDMiddleware() RequestIDMiddleware {
	return func(c *gin.Context) {
		var id string = c.GetHeader(RequestIDHeader)
		if !validRequestID(id) {
			id = uuid.NewString()
		}

		c.Set(ContextRequestID, id)

		// Echoed back so a client can quote it in a bug report, and so the
		// caller that supplied one can match our logs to theirs.
		c.Header(RequestIDHeader, id)

		c.Next()
	}
}

// RequestIDFrom returns the request's correlation ID, or "" outside a request.
func RequestIDFrom(c *gin.Context) string {
	var value any
	var exists bool
	value, exists = c.Get(ContextRequestID)
	if !exists {
		return ""
	}

	var id string
	var ok bool
	id, ok = value.(string)
	if !ok {
		return ""
	}
	return id
}

// validRequestID reports whether an inbound value is safe to log and echo.
//
// The alphabet is deliberately narrower than what a UUID or a typical trace ID
// needs: letters, digits, dot, dash, underscore. It excludes every character
// that means something in a log format, a header, or a shell someone later
// pipes the logs through.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLength {
		return false
	}

	var r rune
	for _, r = range id {
		var ok bool = (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.'
		if !ok {
			return false
		}
	}
	return true
}

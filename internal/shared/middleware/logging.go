// File: internal/shared/middleware/logging.go

package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// LoggingMiddleware logs one line per request and puts a request-scoped logger
// on the context.
type LoggingMiddleware gin.HandlerFunc

// slowRequestThreshold is when a request is worth a warning of its own.
const slowRequestThreshold = 2 * time.Second

// NewLoggingMiddleware builds the middleware.
//
// It does two things, and the second is the more useful one. The obvious one is
// the access log: method, path, status, duration, client IP. The other is that
// it derives a logger already carrying the request ID and puts it on the
// request's context — so every `logger.FromContext(ctx)` call anywhere
// downstream, in a service or a repository or the SQL bridge, emits lines that
// correlate with this request without a single one of them being passed an ID.
//
// It must run after the request-ID middleware, which is what it reads.
func NewLoggingMiddleware(base zerolog.Logger) LoggingMiddleware {
	return func(c *gin.Context) {
		var start time.Time = time.Now()

		var requestID string = RequestIDFrom(c)

		var scoped zerolog.Logger = base.With().
			Str("request_id", requestID).
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Logger()

		c.Request = c.Request.WithContext(logger.WithContext(c.Request.Context(), scoped))

		c.Next()

		var elapsed time.Duration = time.Since(start)
		var status int = c.Writer.Status()

		var event *zerolog.Event
		switch {
		case status >= 500:
			event = scoped.Error()
		case status >= 400:
			event = scoped.Warn()
		case elapsed >= slowRequestThreshold:
			event = scoped.Warn().Dur("threshold", slowRequestThreshold)
		default:
			event = scoped.Info()
		}

		event = event.
			Int("status", status).
			Dur("duration", elapsed).
			Int("bytes", c.Writer.Size()).
			Str("ip", c.ClientIP())

		// The query string is logged, the body is not. A query string is
		// already in the access log of everything else on the path; a body
		// routinely contains a password.
		if c.Request.URL.RawQuery != "" {
			event = event.Str("query", c.Request.URL.RawQuery)
		}

		// Errors gin collected — including the ones the handlers attach — are
		// summarised rather than rendered, so nothing here can leak a cause the
		// response deliberately withheld.
		if len(c.Errors) > 0 {
			event = event.Int("errors", len(c.Errors))
		}

		event.Msg("request")
	}
}

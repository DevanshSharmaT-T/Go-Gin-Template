// File: internal/shared/middleware/recovery.go

package middleware

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// RecoveryMiddleware turns a panic into a 500 instead of a dead process.
type RecoveryMiddleware gin.HandlerFunc

// NewRecoveryMiddleware builds the middleware.
//
// It is the outermost middleware, so it covers the others as well as the
// handler. Gin ships its own; this one exists because Gin's writes an
// unstructured trace to stdout and, in debug mode, the panic value to the
// client.
//
// **The panic value never reaches the response.** A panic message routinely
// contains a file path, a struct dump or a query — the runtime writes whatever
// was being processed — and a client that can trigger a panic should not also
// receive its contents. It goes to the log and to error tracking; the caller
// gets the same generic 500 as any other internal failure.
func NewRecoveryMiddleware() RecoveryMiddleware {
	return func(c *gin.Context) {
		defer func() {
			var recovered any = recover()
			if recovered == nil {
				return
			}

			var log = logger.FromContext(c.Request.Context())

			// A broken pipe is the client having gone away mid-response. There
			// is nothing wrong on this side, nothing to report, and no
			// connection left to write a status onto.
			if isBrokenPipe(recovered) {
				log.Warn().
					Str("path", c.Request.URL.Path).
					Msg("connection closed by the client mid-response")
				c.Abort()
				return
			}

			var err error = asError(recovered)

			// The stack is the whole point of catching this, so it is attached
			// as a field rather than left to be reconstructed from a message.
			// CaptureError writes to the log and to error tracking together,
			// so a panic cannot reach one and not the other.
			var enriched zerolog.Logger = log.With().
				Str("stack", string(debug.Stack())).
				Str("path", c.Request.URL.Path).
				Str("method", c.Request.Method).
				Str("request_id", RequestIDFrom(c)).
				Logger()

			logger.CaptureError(&enriched, err, "panic recovered while handling a request")

			c.Abort()
			var appErr *apperrors.AppError = apperrors.NewInternalError("panic recovered", err).
				WithRequestID(RequestIDFrom(c))
			c.JSON(http.StatusInternalServerError, appErr.Response())
		}()

		c.Next()
	}
}

// asError normalises whatever was panicked into an error.
func asError(recovered any) error {
	var err error
	var ok bool
	err, ok = recovered.(error)
	if ok {
		return err
	}
	return fmt.Errorf("%v", recovered)
}

// isBrokenPipe reports whether a panic is the client having disconnected.
func isBrokenPipe(recovered any) bool {
	var netErr *net.OpError
	var err error
	var ok bool

	err, ok = recovered.(error)
	if !ok {
		return false
	}
	if errors.Is(err, http.ErrAbortHandler) {
		return true
	}
	if !errors.As(err, &netErr) {
		return false
	}

	var sysErr *os.SyscallError
	if !errors.As(netErr.Err, &sysErr) {
		return false
	}

	var message string = strings.ToLower(sysErr.Error())
	return strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "connection reset by peer")
}

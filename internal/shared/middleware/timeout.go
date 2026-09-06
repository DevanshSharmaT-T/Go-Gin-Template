// File: internal/shared/middleware/timeout.go

package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// TimeoutMiddleware bounds how long a request may take.
type TimeoutMiddleware gin.HandlerFunc

// NewTimeoutMiddleware builds the middleware.
//
// # Why this is more than a context deadline
//
// Setting a deadline on the request context is the important half: it
// propagates into the database driver, so a slow query is cancelled rather than
// left running for a client who has gone. But a handler that ignores its
// context — a tight loop, a call into a library that does not take one — would
// still run forever, and the caller would wait for it.
//
// So the handler runs in its own goroutine and this one waits. On timeout it
// writes a 504 and returns, and the handler is left to finish into a buffer
// nobody reads.
//
// # Two races, and how each is closed
//
// **The response.** The handler is still running when the timeout fires, and it
// is about to write. Two goroutines writing to one ResponseWriter is a data
// race and produces a response with two status codes. So the handler never
// touches the real writer: it writes into [timeoutWriter], which buffers under
// a mutex. Exactly one of two things then happens, decided under that same
// mutex — either the handler finishes and its buffered response is flushed, or
// the timeout fires, the buffer is discarded and a 504 is written. This is the
// shape of the standard library's http.TimeoutHandler, for the same reasons.
//
// **The context.** `gin.Context` is not safe for concurrent use — `c.Next()`
// advances an index on it — so this middleware must not return while the
// handler goroutine is still inside the chain. Returning would let gin's outer
// loop walk the same context the abandoned handler is still walking, which the
// race detector reports immediately.
//
// So after writing the 504 it **waits for the handler to finish**. The client
// already has its response; only this goroutine lingers, and only for as long
// as a handler ignores a context that has already been cancelled. That is the
// honest trade: a lingering goroutine is a leak worth watching, a data race in
// the request path is not a trade at all.
//
// REQUEST_TIMEOUT is validated at startup to be shorter than
// SERVER_WRITE_TIMEOUT, because otherwise the server tears the connection down
// before this 504 can be written.
func NewTimeoutMiddleware(cfg *config.Config) TimeoutMiddleware {
	var timeout = cfg.Server.RequestTimeout

	return func(c *gin.Context) {
		if timeout <= 0 {
			c.Next()
			return
		}

		var ctx context.Context
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)

		var original gin.ResponseWriter = c.Writer
		var writer *timeoutWriter = newTimeoutWriter(original)
		c.Writer = writer

		// Read before the goroutine starts. Everything below reads only these
		// copies, never c, until the handler has finished.
		var path string = c.Request.URL.Path
		var requestCtx context.Context = c.Request.Context()
		var requestID string = RequestIDFrom(c)

		var finished chan struct{} = make(chan struct{})
		var panicked chan any = make(chan any, 1)

		go func() {
			// Registered first, so it runs last: finished always closes, even
			// when the handler panics. Waiting on it can therefore never hang.
			defer close(finished)
			defer func() {
				var recovered any = recover()
				if recovered != nil {
					panicked <- recovered
				}
			}()
			c.Next()
		}()

		var recovered any

		select {
		case <-finished:
			select {
			case recovered = <-panicked:
				// Put the real writer back before re-panicking, or the
				// recovery middleware's 500 lands in a buffer that is never
				// flushed and the client gets a 200 with no body.
				c.Writer = original
				// Re-raised on this goroutine so recovery, which is outside
				// this middleware, sees it. A panic left on the handler's
				// goroutine would take the process down.
				panic(recovered)
			default:
				writer.flush()
			}

		case <-ctx.Done():
			writer.timeout(requestCtx, path, requestID)

			// See "The context" above: do not return while the handler is
			// still inside c.Next().
			<-finished

			select {
			case recovered = <-panicked:
				// The 504 is already on the wire, so re-panicking would only
				// produce a superfluous second write. Log it with the same
				// weight recovery would.
				logger.FromContext(requestCtx).Error().
					Interface("panic", recovered).
					Str("path", path).
					Msg("abandoned handler panicked after its request had timed out")
			default:
			}
		}
	}
}

// timeoutWriter buffers a handler's response so that it and the timeout cannot
// write at the same time.
type timeoutWriter struct {
	gin.ResponseWriter

	mu       sync.Mutex
	buffer   bytes.Buffer
	headers  http.Header
	status   int
	timedOut bool
	flushed  bool
}

// newTimeoutWriter wraps the real writer.
func newTimeoutWriter(original gin.ResponseWriter) *timeoutWriter {
	return &timeoutWriter{
		ResponseWriter: original,
		headers:        make(http.Header),
		status:         http.StatusOK,
	}
}

// Header returns the buffered header map, so a handler setting a header after
// the timeout does not mutate a response already sent.
func (w *timeoutWriter) Header() http.Header {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.headers
}

// Write buffers the body.
func (w *timeoutWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timedOut {
		// Discarded, but reported as written: returning an error here would
		// send the handler down an error path for a response nobody will read,
		// and that path usually tries to write again.
		return len(data), nil
	}
	return w.buffer.Write(data)
}

// WriteString buffers the body.
func (w *timeoutWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// WriteHeader records the status.
func (w *timeoutWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timedOut {
		return
	}
	w.status = status
}

// WriteHeaderNow is part of gin.ResponseWriter. Buffering means there is
// nothing to send yet, so it is deliberately a no-op.
func (w *timeoutWriter) WriteHeaderNow() {}

// Status reports the status that was or will be sent, which is what the logging
// middleware reads after this middleware returns.
func (w *timeoutWriter) Status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

// Size reports the buffered body length.
func (w *timeoutWriter) Size() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.Len()
}

// Written reports whether anything has been produced.
func (w *timeoutWriter) Written() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushed || w.buffer.Len() > 0
}

// flush copies the buffered response onto the real writer. It runs only after
// the handler has finished.
func (w *timeoutWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timedOut || w.flushed {
		return
	}
	w.flushed = true

	var key string
	var values []string
	for key, values = range w.headers {
		var value string
		for _, value = range values {
			w.ResponseWriter.Header().Add(key, value)
		}
	}

	w.ResponseWriter.WriteHeader(w.status)
	if w.buffer.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.buffer.Bytes())
	}
}

// timeout discards the buffer and writes a 504.
//
// It takes the values it needs rather than the gin.Context, because the handler
// goroutine still owns that context and reading it here would be the same race
// this middleware exists to avoid.
func (w *timeoutWriter) timeout(ctx context.Context, path string, requestID string) {
	w.mu.Lock()

	if w.flushed {
		// The handler won the race by a hair: its response is already on the
		// wire, so there is nothing to time out.
		w.mu.Unlock()
		return
	}

	w.timedOut = true
	w.buffer.Reset()
	w.status = http.StatusGatewayTimeout
	w.mu.Unlock()

	var appErr *apperrors.AppError = apperrors.NewTimeoutError(
		"the request took too long to complete", context.DeadlineExceeded).
		WithRequestID(requestID)

	var body []byte
	var err error
	body, err = json.Marshal(appErr.Response())
	if err != nil {
		body = []byte(`{"type":"TIMEOUT","error":"the request timed out"}`)
	}

	logger.FromContext(ctx).Warn().
		Str("path", path).
		Msg("request exceeded REQUEST_TIMEOUT and was abandoned")

	w.ResponseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.ResponseWriter.WriteHeader(http.StatusGatewayTimeout)
	_, _ = w.ResponseWriter.Write(body)
}

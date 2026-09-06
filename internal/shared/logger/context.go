// File: internal/shared/logger/context.go

package logger

import (
	"context"

	"github.com/rs/zerolog"
)

// contextKey is unexported so no other package can collide with this key. A
// string literal in a shared context would eventually be reused by someone
// else; a private type cannot be.
type contextKey struct{}

// loggerKey identifies the request-scoped logger inside a context.
var loggerKey contextKey = contextKey{}

// WithContext returns a copy of ctx carrying l.
//
// The intended use is one enriched logger per request — with the request ID,
// method, route and authenticated user already attached — so that every line
// emitted while handling that request correlates without each call site
// repeating the fields.
func WithContext(ctx context.Context, l zerolog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// FromContext returns the request-scoped logger, or the package default when
// the context does not carry one.
//
// It never returns nil and never returns a no-op logger: code that logs an
// error should not have that error vanish because a context was not threaded
// through properly.
//
// The result is a pointer because zerolog's level methods have pointer
// receivers, which makes logger.FromContext(ctx).Info() work directly. Do not
// mutate it — derive from it with With() instead.
func FromContext(ctx context.Context) *zerolog.Logger {
	if ctx == nil {
		return Default()
	}

	var l zerolog.Logger
	var ok bool
	l, ok = ctx.Value(loggerKey).(zerolog.Logger)
	if !ok {
		return Default()
	}
	return &l
}

// File: internal/shared/logger/sentry.go

package logger

import (
	"time"

	sentry "github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"
)

// Sentry is optional throughout. Leave the DSN empty and InitSentry installs
// nothing, returns a no-op flush, and the rest of the application is unchanged.
//
// What gets reported is decided at the transport layer, not here: only failures
// that map to HTTP 5xx are captured. Reporting 4xx as well buries genuine
// faults under ordinary client mistakes, which is how an issue feed stops being
// read.

// SentryOptions configures error tracking.
type SentryOptions struct {
	// DSN is the project key. Empty disables Sentry entirely.
	DSN string

	// Environment separates production from staging in the Sentry UI.
	Environment string

	// Release ties an event to a build. Setting it is what makes "this started
	// at 14:02 with deploy abc123" answerable.
	Release string

	// TracesSampleRate is the fraction of transactions sampled for performance
	// monitoring, 0.0 to 1.0. Zero means errors only.
	TracesSampleRate float64

	// Debug logs Sentry's own transport activity. Useful exactly once, when
	// events are not arriving.
	Debug bool
}

// FlushFunc drains buffered events. Sentry's transport is asynchronous, so a
// process that exits without flushing loses whatever had not been sent —
// including, typically, the fatal error that caused the exit.
type FlushFunc func()

// InitSentry installs the Sentry client and returns a flush function to call
// during shutdown.
//
// When opts.DSN is empty it returns a no-op flush and a nil error, so callers
// never need to branch on whether error tracking is configured.
func InitSentry(opts SentryOptions) (FlushFunc, error) {
	if opts.DSN == "" {
		return func() {}, nil
	}

	var err error = sentry.Init(sentry.ClientOptions{
		Dsn:              opts.DSN,
		Environment:      opts.Environment,
		Release:          opts.Release,
		EnableTracing:    opts.TracesSampleRate > 0,
		TracesSampleRate: opts.TracesSampleRate,
		Debug:            opts.Debug,
		AttachStacktrace: true,
	})
	if err != nil {
		return func() {}, err
	}

	return func() {
		sentry.Flush(sentryFlushTimeout)
	}, nil
}

// sentryFlushTimeout bounds how long shutdown waits for buffered events. It is
// deliberately short: losing a report is better than hanging a deployment.
const sentryFlushTimeout = 2 * time.Second

// CaptureError reports err to Sentry and records it on the given logger.
//
// It is the single place both destinations are written, so a failure cannot end
// up in the issue tracker without also appearing in the logs — which is where
// anyone debugging it will look first.
func CaptureError(l *zerolog.Logger, err error, message string) {
	if err == nil {
		return
	}
	if l == nil {
		l = Default()
	}

	l.Error().Err(err).Msg(message)
	sentry.CaptureException(err)
}

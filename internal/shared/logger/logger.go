// File: internal/shared/logger/logger.go

// Package logger builds the application's structured logger and carries a
// request-scoped copy of it through context.
//
// Structured logging is the difference between grepping strings and querying
// fields. Every line this package produces is a JSON object with a level, a
// timestamp and whatever fields the call site attached, so "show me every
// failed login for this user in the last hour" is a query rather than an
// archaeology exercise.
//
// The package deliberately does not depend on internal/config: it takes a small
// Options value instead. That keeps the shared kernel free of a dependency on
// the application's own configuration shape, and makes the logger usable from a
// test with two lines of setup.
package logger

import (
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// Format selects the output encoding.
const (
	// FormatJSON writes one object per line. Use it anywhere logs are shipped
	// to an aggregator.
	FormatJSON = "json"
	// FormatConsole writes coloured, human-readable lines. Development only —
	// it is slower and not machine-parseable.
	FormatConsole = "console"
)

// Options configures a logger. The zero value is usable: it produces an
// info-level JSON logger on stderr.
type Options struct {
	// Level is one of trace, debug, info, warn, error, fatal, panic. An
	// unrecognised value falls back to info rather than failing, because losing
	// logs is a worse outcome than logging too much.
	Level string

	// Format is FormatJSON or FormatConsole.
	Format string

	// AppName and Environment are attached to every line, so logs from several
	// services in one stream stay separable.
	AppName     string
	Environment string

	// Output defaults to os.Stderr. Logs go to stderr, not stdout, so that a
	// command whose stdout is real output stays pipeable.
	Output io.Writer
}

// New builds a logger from opts.
//
// Timestamps are RFC3339 with nanoseconds: sortable lexicographically, and
// precise enough to order events inside a single fast request.
func New(opts Options) zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano

	var output io.Writer = opts.Output
	if output == nil {
		output = os.Stderr
	}

	if strings.EqualFold(opts.Format, FormatConsole) {
		output = zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: "15:04:05.000",
		}
	}

	var context zerolog.Context = zerolog.New(output).With().Timestamp()
	if opts.AppName != "" {
		context = context.Str("app", opts.AppName)
	}
	if opts.Environment != "" {
		context = context.Str("env", opts.Environment)
	}

	return context.Logger().Level(parseLevel(opts.Level))
}

// parseLevel maps a level name onto zerolog's level, falling back to info.
func parseLevel(level string) zerolog.Level {
	if level == "" {
		return zerolog.InfoLevel
	}

	var parsed zerolog.Level
	var err error
	parsed, err = zerolog.ParseLevel(strings.ToLower(level))
	if err != nil {
		return zerolog.InfoLevel
	}
	return parsed
}

// defaultLogger is the fallback used by FromContext when no request-scoped
// logger is present.
//
// A package-level default is a deliberate compromise. The alternative — return
// a no-op logger — silently discards output from any code path that forgot to
// thread the context through, and silent log loss is the hardest kind of bug to
// notice. This way such a line still lands on stderr.
//
// It is an atomic pointer rather than a plain variable so that SetDefault
// during startup cannot race with a goroutine that is already logging.
var defaultLogger atomic.Pointer[zerolog.Logger]

func init() {
	var initial zerolog.Logger = New(Options{Level: "info", Format: FormatJSON})
	defaultLogger.Store(&initial)
}

// SetDefault replaces the fallback logger. Call it once, at startup, after the
// real configuration has been read.
func SetDefault(l zerolog.Logger) {
	defaultLogger.Store(&l)
}

// Default returns the fallback logger.
//
// It returns a pointer because zerolog's level methods (Info, Error, ...) have
// pointer receivers, so a value return would force every caller to assign to a
// variable before it could log. Do not mutate the result — derive from it with
// With() instead.
func Default() *zerolog.Logger {
	return defaultLogger.Load()
}

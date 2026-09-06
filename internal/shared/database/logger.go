// File: internal/shared/database/logger.go

package database

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	applog "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// GORM ships its own logger, writing unstructured lines to stdout. This bridge
// replaces it so that database output is the same JSON as everything else, and
// so that a query logged during a request carries that request's fields —
// request ID included — because it is drawn from the context GORM already has.
//
// DATABASE_LOG_LEVEL is the volume control:
//
//	silent   nothing at all
//	error    failed statements only
//	warn     the above, plus statements slower than slowQueryThreshold  (default)
//	info     every statement
//
// The severity each line is emitted at matches what it means — a failure logs
// at error, a slow query at warn, an ordinary one at info — so LOG_LEVEL does
// not have to be lowered as well to see something you explicitly asked for.

// slowQueryThreshold is how long a statement may take before it is worth a
// warning. It matches GORM's own default and is a constant rather than a
// setting: an operator who wants more detail has DATABASE_LOG_LEVEL, and one
// more environment variable to explain is not worth the tuning knob.
const slowQueryThreshold = 200 * time.Millisecond

// gormLogger adapts zerolog to gorm's logger.Interface.
type gormLogger struct {
	level gormlogger.LogLevel
}

// newGormLogger builds the bridge for a DATABASE_LOG_LEVEL value. The value has
// already been validated by internal/config; an unrecognised one falls back to
// warn rather than failing, because losing logs is worse than logging too much.
func newGormLogger(level string) gormlogger.Interface {
	return &gormLogger{level: parseGormLogLevel(level)}
}

// parseGormLogLevel maps a DATABASE_LOG_LEVEL value onto GORM's level.
func parseGormLogLevel(level string) gormlogger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "silent":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "warn":
		return gormlogger.Warn
	case "info":
		return gormlogger.Info
	default:
		return gormlogger.Warn
	}
}

// LogMode returns a copy at a different level. GORM calls it on session
// creation, so it must not mutate the receiver — sessions would otherwise leak
// a level change into every other query in the process.
func (l *gormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	var clone gormLogger = *l
	clone.level = level
	return &clone
}

// Info logs one of GORM's own informational messages.
func (l *gormLogger) Info(ctx context.Context, msg string, args ...any) {
	if l.level < gormlogger.Info {
		return
	}
	emit(applog.FromContext(ctx).Info(), msg, args)
}

// Warn logs one of GORM's own warnings.
func (l *gormLogger) Warn(ctx context.Context, msg string, args ...any) {
	if l.level < gormlogger.Warn {
		return
	}
	emit(applog.FromContext(ctx).Warn(), msg, args)
}

// Error logs one of GORM's own errors.
func (l *gormLogger) Error(ctx context.Context, msg string, args ...any) {
	if l.level < gormlogger.Error {
		return
	}
	emit(applog.FromContext(ctx).Error(), msg, args)
}

// Trace is called once per statement with the SQL, the row count and the error.
//
// gorm.ErrRecordNotFound is never logged as a failure: "no row matched" is an
// ordinary result that the repository turns into a NOT_FOUND, and logging it at
// error level fills the log with successful 404s.
func (l *gormLogger) Trace(
	ctx context.Context,
	begin time.Time,
	fc func() (sql string, rowsAffected int64),
	err error,
) {
	if l.level <= gormlogger.Silent {
		return
	}

	var elapsed time.Duration = time.Since(begin)
	var failed bool = err != nil && !errors.Is(err, gorm.ErrRecordNotFound)

	var event *zerolog.Event
	var message string
	switch {
	case failed && l.level >= gormlogger.Error:
		event = applog.FromContext(ctx).Error().Err(err)
		message = "database statement failed"
	case elapsed >= slowQueryThreshold && l.level >= gormlogger.Warn:
		event = applog.FromContext(ctx).Warn().Dur("threshold", slowQueryThreshold)
		message = "slow database statement"
	case l.level >= gormlogger.Info:
		event = applog.FromContext(ctx).Info()
		message = "database statement"
	default:
		return
	}

	var sql string
	var rows int64
	sql, rows = fc()
	event.Str("sql", sql).Int64("rows", rows).Dur("elapsed", elapsed).Msg(message)
}

// ParamsFilter is called by GORM before it renders a statement for logging.
//
// It implements gorm.ParamsFilter, and returning no parameters is what keeps
// bound values out of the log: the line carries the numbered placeholder, never
// the address that was substituted into it. This is the same thing GORM's own
// logger does under ParameterizedQueries, made unconditional.
//
// It is unconditional because the arguments to an ordinary query are exactly
// the data the application exists to protect — email addresses, tokens, the
// plaintext side of a credential check. A log aggregator is not the place for
// them, and "only in development" is not a promise a log level can keep. If you
// need the values while debugging, delete this method: GORM falls back to
// rendering them.
func (l *gormLogger) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}

// emit writes msg, treating it as a format string only when GORM supplied
// arguments for it. GORM's own messages contain verbs; ours do not.
func emit(event *zerolog.Event, msg string, args []any) {
	if len(args) == 0 {
		event.Msg(msg)
		return
	}
	event.Msgf(msg, args...)
}

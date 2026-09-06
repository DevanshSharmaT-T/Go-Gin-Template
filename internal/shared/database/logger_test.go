// File: internal/shared/database/logger_test.go

package database

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	applog "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// captureContext returns a context carrying a logger that writes into buf, so a
// test can read exactly what a query would have produced. Nothing global is
// touched, which is what lets these run in any order.
func captureContext(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()

	buf := &bytes.Buffer{}
	l := applog.New(applog.Options{Level: "trace", Format: applog.FormatJSON, Output: buf})
	return applog.WithContext(context.Background(), l), buf
}

// decode reads the single JSON line the logger produced.
func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("expected a log line, got none")
	}

	var fields map[string]any
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, line)
	}
	return fields
}

func TestParseGormLogLevel_MapsEveryConfiguredValue(t *testing.T) {
	cases := []struct {
		in   string
		want gormlogger.LogLevel
	}{
		{"silent", gormlogger.Silent},
		{"error", gormlogger.Error},
		{"warn", gormlogger.Warn},
		{"info", gormlogger.Info},

		// internal/config validates the value, so these can only come from a
		// caller building the bridge by hand. Falling back beats failing:
		// losing logs is worse than logging too much.
		{"WARN", gormlogger.Warn},
		{"  info  ", gormlogger.Info},
		{"", gormlogger.Warn},
		{"verbose", gormlogger.Warn},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := parseGormLogLevel(tc.in); got != tc.want {
				t.Fatalf("parseGormLogLevel(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// GORM calls LogMode when it creates a session. Mutating the receiver would let
// one session's level change leak into every other query in the process.
func TestGormLogger_LogMode_DoesNotMutateTheReceiver(t *testing.T) {
	original := &gormLogger{level: gormlogger.Warn}

	derived := original.LogMode(gormlogger.Silent)

	if original.level != gormlogger.Warn {
		t.Fatalf("LogMode mutated the receiver: level is now %v", original.level)
	}
	if derived.(*gormLogger).level != gormlogger.Silent {
		t.Fatalf("want the copy at Silent, got %v", derived.(*gormLogger).level)
	}
}

func TestGormLogger_Trace_LogsFailuresAtError(t *testing.T) {
	ctx, buf := captureContext(t)
	l := &gormLogger{level: gormlogger.Warn}

	l.Trace(ctx, time.Now(), func() (string, int64) {
		return `INSERT INTO users (email) VALUES ($1)`, 0
	}, errors.New("boom"))

	fields := decode(t, buf)
	if fields["level"] != "error" {
		t.Fatalf("want level error, got %v", fields["level"])
	}
	if fields["sql"] != `INSERT INTO users (email) VALUES ($1)` {
		t.Fatalf("want the statement in the line, got %v", fields["sql"])
	}
}

// "No row matched" is an ordinary result that becomes a NOT_FOUND upstream.
// Logging it at error level fills the log with successful 404s.
func TestGormLogger_Trace_DoesNotTreatRecordNotFoundAsAFailure(t *testing.T) {
	ctx, buf := captureContext(t)
	l := &gormLogger{level: gormlogger.Warn}

	l.Trace(ctx, time.Now(), func() (string, int64) {
		return `SELECT * FROM users WHERE id = $1`, 0
	}, gorm.ErrRecordNotFound)

	if line := strings.TrimSpace(buf.String()); line != "" {
		t.Fatalf("want no output at warn level, got %q", line)
	}
}

func TestGormLogger_Trace_WarnsAboutSlowStatements(t *testing.T) {
	ctx, buf := captureContext(t)
	l := &gormLogger{level: gormlogger.Warn}

	l.Trace(ctx, time.Now().Add(-2*slowQueryThreshold), func() (string, int64) {
		return `SELECT count(*) FROM users`, 1
	}, nil)

	fields := decode(t, buf)
	if fields["level"] != "warn" {
		t.Fatalf("want level warn, got %v", fields["level"])
	}
}

// warn is the default, and it is the level at which an ordinary statement must
// stay unlogged — otherwise every deployment logs every query.
func TestGormLogger_Trace_IsQuietAtWarnForAnOrdinaryStatement(t *testing.T) {
	ctx, buf := captureContext(t)
	l := &gormLogger{level: gormlogger.Warn}

	l.Trace(ctx, time.Now(), func() (string, int64) {
		return `SELECT 1`, 1
	}, nil)

	if line := strings.TrimSpace(buf.String()); line != "" {
		t.Fatalf("want no output at warn level, got %q", line)
	}
}

func TestGormLogger_Trace_LogsEveryStatementAtInfo(t *testing.T) {
	ctx, buf := captureContext(t)
	l := &gormLogger{level: gormlogger.Info}

	l.Trace(ctx, time.Now(), func() (string, int64) {
		return `SELECT 1`, 1
	}, nil)

	fields := decode(t, buf)
	if fields["level"] != "info" {
		t.Fatalf("want level info, got %v", fields["level"])
	}
}

func TestGormLogger_Trace_SilentLogsNothingAtAll(t *testing.T) {
	ctx, buf := captureContext(t)
	l := &gormLogger{level: gormlogger.Silent}

	l.Trace(ctx, time.Now().Add(-time.Hour), func() (string, int64) {
		return `SELECT 1`, 1
	}, errors.New("boom"))

	if line := strings.TrimSpace(buf.String()); line != "" {
		t.Fatalf("want no output at silent level, got %q", line)
	}
}

// The arguments to a query are the data the application exists to protect.
// Returning no parameters is what makes GORM render `$1` rather than the value.
func TestGormLogger_ParamsFilter_DropsBoundValues(t *testing.T) {
	l := &gormLogger{level: gormlogger.Info}

	sql, params := l.ParamsFilter(
		context.Background(),
		`SELECT * FROM users WHERE email = $1`,
		"someone@example.com",
	)

	if sql != `SELECT * FROM users WHERE email = $1` {
		t.Fatalf("the statement should be unchanged, got %q", sql)
	}
	if len(params) != 0 {
		t.Fatalf("want no parameters, got %v", params)
	}
}

// GORM's own messages carry printf verbs; ours do not. Treating a message
// without arguments as a format string would mangle any statement containing a
// literal percent sign.
func TestGormLogger_Warn_DoesNotFormatAMessageWithNoArguments(t *testing.T) {
	ctx, buf := captureContext(t)
	l := &gormLogger{level: gormlogger.Warn}

	l.Warn(ctx, "100% of connections are in use")

	fields := decode(t, buf)
	if fields["message"] != "100% of connections are in use" {
		t.Fatalf("message was reformatted: %v", fields["message"])
	}
}

func TestGormLogger_Info_IsSuppressedBelowItsLevel(t *testing.T) {
	ctx, buf := captureContext(t)
	l := &gormLogger{level: gormlogger.Warn}

	l.Info(ctx, "something GORM wanted to mention")

	if line := strings.TrimSpace(buf.String()); line != "" {
		t.Fatalf("want no output, got %q", line)
	}
}

// Guard against the bridge drifting away from the interface GORM expects.
var _ gormlogger.Interface = (*gormLogger)(nil)

var _ gorm.ParamsFilter = (*gormLogger)(nil)

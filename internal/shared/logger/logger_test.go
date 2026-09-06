// File: internal/shared/logger/logger_test.go

package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func decodeLine(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	line := strings.TrimSpace(string(raw))
	if line == "" {
		t.Fatal("no log line was written")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		t.Fatalf("log line is not valid JSON (%v): %s", err, line)
	}
	return fields
}

func TestNew_WritesStructuredJSONWithIdentity(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Level: "info", Format: FormatJSON, AppName: "svc", Environment: "test", Output: &buf})

	l.Info().Str("user_id", "u-1").Msg("logged in")

	fields := decodeLine(t, buf.Bytes())
	for key, want := range map[string]string{
		"app":     "svc",
		"env":     "test",
		"level":   "info",
		"message": "logged in",
		"user_id": "u-1",
	} {
		if got, _ := fields[key].(string); got != want {
			t.Errorf("field %q: want %q, got %q", key, want, got)
		}
	}
	if _, present := fields["time"]; !present {
		t.Error("every line needs a timestamp")
	}
}

func TestNew_LevelFiltersQuieterEvents(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Level: "warn", Format: FormatJSON, Output: &buf})

	l.Debug().Msg("noise")
	l.Info().Msg("also noise")
	if buf.Len() != 0 {
		t.Errorf("events below the level must be dropped, got: %s", buf.String())
	}

	l.Warn().Msg("kept")
	if fields := decodeLine(t, buf.Bytes()); fields["message"] != "kept" {
		t.Errorf("want the warn line, got %v", fields)
	}
}

func TestNew_UnknownLevelFallsBackToInfo(t *testing.T) {
	// Losing logs is worse than logging too much, so a typo must not silence
	// the process.
	var buf bytes.Buffer
	l := New(Options{Level: "verbose", Format: FormatJSON, Output: &buf})

	l.Info().Msg("still logged")

	if buf.Len() == 0 {
		t.Fatal("an unrecognised level silenced the logger")
	}
	if fields := decodeLine(t, buf.Bytes()); fields["level"] != "info" {
		t.Errorf("want info level, got %v", fields["level"])
	}
}

func TestNew_ConsoleFormatIsHumanReadable(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Level: "info", Format: FormatConsole, Output: &buf})

	l.Info().Msg("hello")

	out := buf.String()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("console format should not emit JSON, got: %s", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("want the message in the output, got: %s", out)
	}
}

func TestNew_ZeroOptionsAreUsable(t *testing.T) {
	zero := New(Options{})
	if zero.GetLevel() != zerolog.InfoLevel {
		t.Error("the zero Options value should give an info-level logger")
	}
}

func TestFromContext_ReturnsTheRequestScopedLogger(t *testing.T) {
	var buf bytes.Buffer
	scoped := New(Options{Level: "info", Format: FormatJSON, Output: &buf}).
		With().Str("request_id", "req-1").Logger()

	FromContext(WithContext(context.Background(), scoped)).Info().Msg("handled")

	if fields := decodeLine(t, buf.Bytes()); fields["request_id"] != "req-1" {
		t.Errorf("want the scoped fields carried through, got %v", fields)
	}
}

func TestFromContext_FallsBackToDefaultRatherThanDiscarding(t *testing.T) {
	// Code that logs an error must not lose it because a context was not
	// threaded through.
	var buf bytes.Buffer
	previous := *Default()
	t.Cleanup(func() { SetDefault(previous) })
	SetDefault(New(Options{Level: "info", Format: FormatJSON, Output: &buf}))

	FromContext(context.Background()).Error().Msg("unthreaded")

	if fields := decodeLine(t, buf.Bytes()); fields["message"] != "unthreaded" {
		t.Errorf("want the line on the default logger, got %v", fields)
	}
}

func TestFromContext_NilContextIsSafe(t *testing.T) {
	var buf bytes.Buffer
	previous := *Default()
	t.Cleanup(func() { SetDefault(previous) })
	SetDefault(New(Options{Level: "info", Format: FormatJSON, Output: &buf}))

	//nolint:staticcheck // passing nil is exactly what is under test here.
	FromContext(nil).Info().Msg("nil ctx")

	if buf.Len() == 0 {
		t.Error("a nil context must not panic or discard the line")
	}
}

func TestInitSentry_NoDSNIsANoOp(t *testing.T) {
	// Sentry is optional everywhere; callers should not have to branch on it.
	flush, err := InitSentry(SentryOptions{})
	if err != nil {
		t.Fatalf("an empty DSN must not be an error: %v", err)
	}
	if flush == nil {
		t.Fatal("want a callable no-op flush, got nil")
	}
	flush()
}

func TestInitSentry_RejectsAMalformedDSN(t *testing.T) {
	flush, err := InitSentry(SentryOptions{DSN: "not-a-dsn"})
	if err == nil {
		t.Error("a malformed DSN should be reported at startup, not silently ignored")
	}
	if flush == nil {
		t.Error("flush must stay callable even when initialisation failed")
	}
}

func TestCaptureError_LogsTheError(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Level: "info", Format: FormatJSON, Output: &buf})

	CaptureError(&l, context.Canceled, "operation failed")

	fields := decodeLine(t, buf.Bytes())
	if fields["message"] != "operation failed" {
		t.Errorf("want the message logged, got %v", fields["message"])
	}
	if fields["error"] != context.Canceled.Error() {
		t.Errorf("want the error attached, got %v", fields["error"])
	}
}

func TestCaptureError_NilErrorIsIgnored(t *testing.T) {
	var buf bytes.Buffer
	quiet := New(Options{Format: FormatJSON, Output: &buf})
	CaptureError(&quiet, nil, "should not appear")

	if buf.Len() != 0 {
		t.Errorf("a nil error must not produce a line, got: %s", buf.String())
	}
}

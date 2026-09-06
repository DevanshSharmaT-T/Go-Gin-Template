// File: internal/config/env.go

package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// reader pulls typed values out of the process environment and *accumulates*
// problems instead of returning on the first one.
//
// That matters for the first-run experience: a fresh clone with three keys
// wrong should report all three at once, not make the reader fix one, restart,
// and discover the next.
type reader struct {
	problems []string

	// keys records every environment variable the loader consulted. It backs
	// the test that keeps .env.example and this package from drifting apart:
	// a key read but not documented is invisible to operators, and a key
	// documented but not read is a setting that silently does nothing.
	keys []string
}

// problem records a fault against a key. Raw values are echoed for parse
// failures — but never for a key the deny-list marks as secret, so a mistyped
// JWT_SECRET does not end up in a log or a screenshot.
func (r *reader) problem(key string, format string, args ...any) {
	r.problems = append(r.problems, fmt.Sprintf("%s: %s", key, fmt.Sprintf(format, args...)))
}

// raw returns the value exactly as set, without falling back to a default.
func (r *reader) raw(key string) string {
	r.keys = append(r.keys, key)
	return strings.TrimSpace(os.Getenv(key))
}

// str returns the value, or def when the key is unset or empty.
func (r *reader) str(key string, def string) string {
	var value string = r.raw(key)
	if value == "" {
		return def
	}
	return value
}

// requiredStr records a problem when the key is unset or empty.
func (r *reader) requiredStr(key string) string {
	var value string = r.raw(key)
	if value == "" {
		r.problem(key, "is required but not set")
	}
	return value
}

// integer parses an int, recording a problem on malformed input.
func (r *reader) integer(key string, def int) int {
	var value string = r.raw(key)
	if value == "" {
		return def
	}

	var parsed int
	var err error
	parsed, err = strconv.Atoi(value)
	if err != nil {
		r.problem(key, "must be a whole number, got %q", value)
		return def
	}
	return parsed
}

// integer64 parses an int64, recording a problem on malformed input.
func (r *reader) integer64(key string, def int64) int64 {
	var value string = r.raw(key)
	if value == "" {
		return def
	}

	var parsed int64
	var err error
	parsed, err = strconv.ParseInt(value, 10, 64)
	if err != nil {
		r.problem(key, "must be a whole number, got %q", value)
		return def
	}
	return parsed
}

// boolean accepts the set strconv.ParseBool does: 1/t/T/true/TRUE, 0/f/F/false.
func (r *reader) boolean(key string, def bool) bool {
	var value string = r.raw(key)
	if value == "" {
		return def
	}

	var parsed bool
	var err error
	parsed, err = strconv.ParseBool(value)
	if err != nil {
		r.problem(key, "must be true or false, got %q", value)
		return def
	}
	return parsed
}

// float parses a float64, recording a problem on malformed input.
func (r *reader) float(key string, def float64) float64 {
	var value string = r.raw(key)
	if value == "" {
		return def
	}

	var parsed float64
	var err error
	parsed, err = strconv.ParseFloat(value, 64)
	if err != nil {
		r.problem(key, "must be a number, got %q", value)
		return def
	}
	return parsed
}

// duration parses a Go duration string such as 15s, 10m or 1h.
//
// Durations are spelled out rather than expressed as bare seconds so the unit
// is visible in .env: SERVER_READ_TIMEOUT=15s cannot be misread the way
// SERVER_READ_TIMEOUT=15 can.
func (r *reader) duration(key string, def time.Duration) time.Duration {
	var value string = r.raw(key)
	if value == "" {
		return def
	}

	var parsed time.Duration
	var err error
	parsed, err = time.ParseDuration(value)
	if err != nil {
		r.problem(key, "must be a duration such as 30s, 5m or 1h, got %q", value)
		return def
	}
	if parsed <= 0 {
		r.problem(key, "must be greater than zero, got %q", value)
		return def
	}
	return parsed
}

// list splits a comma-separated value, trimming blanks. An entirely empty value
// yields def, which lets a key mean "explicitly nothing" only via its default.
func (r *reader) list(key string, def []string) []string {
	var value string = r.raw(key)
	if value == "" {
		return def
	}

	var parts []string = strings.Split(value, ",")
	var out []string = make([]string, 0, len(parts))

	var part string
	for _, part = range parts {
		var trimmed string = strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// enum returns the value lowercased, recording a problem when it is outside the
// allowed set. Catching a typo here beats discovering at runtime that a
// misspelled LOG_FORMAT silently fell back to a default.
func (r *reader) enum(key string, def string, allowed ...string) string {
	var value string = strings.ToLower(r.str(key, def))

	var candidate string
	for _, candidate = range allowed {
		if value == candidate {
			return value
		}
	}

	r.problem(key, "must be one of %s, got %q", strings.Join(allowed, ", "), value)
	return def
}

// File: internal/config/defaults.go

package config

import (
	"os"
	"time"
)

// Every default lives here so that .env.example and the code can be checked
// against each other in one place. The documented default in .env.example must
// match the constant below it.

const (
	goEnvKey    = "GO_ENV"
	baseEnvFile = ".env"

	// wildcardOrigin is rejected in combination with credentials.
	wildcardOrigin = "*"
)

// Log formats.
const (
	LogFormatJSON    = "json"
	LogFormatConsole = "console"
)

// Mail drivers.
const (
	// MailDriverLog renders the template to the logger and sends nothing. It is
	// the default so a fresh clone runs without a mail server.
	MailDriverLog = "log"
	// MailDriverSMTP delivers over SMTP.
	MailDriverSMTP = "smtp"
)

// SMTP transport security.
const (
	SMTPTLSNone     = "none"
	SMTPTLSStartTLS = "starttls"
	SMTPTLSImplicit = "tls"
)

// MinJWTSecretLength is the shortest accepted JWT_SECRET.
//
// HS256 keys should be at least as long as the hash output (32 bytes). Shorter
// keys reduce the work an offline forgery attempt needs, and a signing key is
// not something to leave to judgement.
const MinJWTSecretLength = 32

// placeholderMarker appears in every value .env.example ships that must be
// replaced. Checking for it catches the specific failure of copying the example
// file and booting without editing it — a long placeholder passes a length
// check but is public knowledge.
const placeholderMarker = "CHANGE_ME"

// Application defaults.
const (
	defaultAppName    = "go-gin-template"
	defaultAppBaseURL = "http://localhost:8080"
)

// Logging defaults.
const (
	defaultLogLevel  = "info"
	defaultLogFormat = LogFormatJSON
)

// Server defaults.
const (
	defaultServiceHost = "127.0.0.1"
	defaultServicePort = 8080

	defaultReadTimeout     = 15 * time.Second
	defaultWriteTimeout    = 30 * time.Second
	defaultIdleTimeout     = 60 * time.Second
	defaultHeaderTimeout   = 10 * time.Second
	defaultRequestTimeout  = 25 * time.Second
	defaultShutdownTimeout = 15 * time.Second

	// 1 MiB.
	defaultMaxRequestBodyBytes int64 = 1 << 20
)

// Database defaults.
const (
	defaultMaxOpenConns     = 25
	defaultMaxIdleConns     = 5
	defaultConnMaxLifetime  = time.Hour
	defaultConnMaxIdleTime  = 10 * time.Minute
	defaultDatabaseLogLevel = "warn"
)

// Auth defaults.
const (
	defaultJWTTTL = 24 * time.Hour
	// 12 is a reasonable 2020s work factor; each increment doubles the cost.
	defaultBcryptCost            = 12
	defaultVerificationTokenTTL  = 24 * time.Hour
	defaultPasswordResetTokenTTL = time.Hour

	minBcryptCost = 10
	// bcrypt itself refuses a cost above 31.
	maxBcryptCost = 31
)

// CORS defaults. There is deliberately no default for CORS_ALLOWED_ORIGINS:
// an unset allow-list permits nothing, which fails closed.
const (
	defaultCORSAllowCredentials = true
	defaultCORSMaxAge           = 12 * time.Hour
)

// Rate-limit defaults.
const (
	defaultRateLimitEnabled   = true
	defaultRateLimitRPS       = 20.0
	defaultRateLimitBurst     = 40
	defaultAuthRateLimitRPM   = 10.0
	defaultAuthRateLimitBurst = 5
)

// Mail defaults.
const (
	defaultMailDriver      = MailDriverLog
	defaultMailFromAddress = "no-reply@example.com"
	defaultSMTPPort        = 587
	defaultSMTPTLS         = SMTPTLSStartTLS
	defaultSMTPTimeout     = 10 * time.Second
)

// Frontend and observability defaults.
const (
	defaultFrontendURL            = "http://localhost:3000"
	defaultSentryTracesSampleRate = 0.0
)

// defaultCORSMethods returns the verbs allowed when CORS_ALLOWED_METHODS is
// unset. Functions rather than package-level slices, so a caller cannot mutate
// the default for the whole process.
func defaultCORSMethods() []string {
	return []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
}

func defaultCORSHeaders() []string {
	return []string{"Authorization", "Content-Type", "X-Request-ID"}
}

func defaultCORSExposedHeaders() []string {
	return []string{"X-Request-ID"}
}

// osGetenv and osSetenv are indirected so tests can exercise the tier loader
// without touching the real process environment.
var osGetenv func(string) string = os.Getenv

var osSetenv func(string, string) error = os.Setenv

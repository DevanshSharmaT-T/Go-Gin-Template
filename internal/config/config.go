// File: internal/config/config.go

// Package config loads, parses and validates every setting the application
// reads, exactly once, into a typed struct.
//
// Two rules hold everywhere else in the codebase:
//
//   - Nothing outside this package calls os.Getenv. If a component needs a
//     setting, it takes it as a parameter.
//   - Invalid configuration is a startup failure, not a runtime surprise. A
//     missing DATABASE_URL, a JWT_SECRET that is still the placeholder, or a
//     CORS wildcard combined with credentials all stop the process with a
//     message naming the key. Booting with a broken security-relevant setting
//     is worse than not booting.
//
// Every key, its default and whether it is required is documented in
// .env.example, which is the reference for operators.
package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Environment names the deployment tier. It selects the .env.<env> file and is
// the default for SENTRY_ENVIRONMENT.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
	EnvStaging     Environment = "staging"
	EnvProduction  Environment = "production"
)

// String makes Environment printable.
func (e Environment) String() string {
	return string(e)
}

// Config is the whole of the application's configuration. It is built once at
// startup and treated as read-only thereafter.
type Config struct {
	App       App
	Log       Log
	Server    Server
	Database  Database
	Auth      Auth
	CORS      CORS
	RateLimit RateLimit
	Mail      Mail
	Frontend  Frontend
	Admin     Admin
	Google    Google
	Sentry    Sentry

	// LoadedFiles lists the .env tier files that were actually found, in the
	// order they were applied. It is reported at startup so "my setting had no
	// effect" is answerable without guessing.
	LoadedFiles []string

	// keysRead is every environment variable consulted during Load. It exists
	// for the test that checks this package against .env.example.
	keysRead []string
}

// App holds identity and environment.
type App struct {
	Env Environment
	// Name feeds the logger, the default JWT issuer and mail subjects.
	Name string
	// BaseURL is the public URL of this API, used to build absolute links.
	BaseURL string
}

// IsDevelopment reports whether this is a developer machine.
func (a App) IsDevelopment() bool { return a.Env == EnvDevelopment }

// IsTest reports whether this is an automated-test run.
func (a App) IsTest() bool { return a.Env == EnvTest }

// IsProduction reports whether this is the production tier.
func (a App) IsProduction() bool { return a.Env == EnvProduction }

// Log holds logging settings.
type Log struct {
	// Level is one of trace, debug, info, warn, error, fatal, panic.
	Level string
	// Format is json (one object per line, for aggregators) or console
	// (coloured and human-readable, for development).
	Format string
}

// Server holds HTTP server and transport-level limits.
//
// Go's http.Server has no default timeouts. Without them a handful of slow
// clients can hold connections open indefinitely and exhaust the pool, so every
// timeout here has a non-zero default.
type Server struct {
	Host string
	Port int

	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
	IdleTimeout   time.Duration
	HeaderTimeout time.Duration

	// RequestTimeout is the per-request deadline applied by middleware. It must
	// be shorter than WriteTimeout, or the connection is torn down before the
	// timeout response can be written.
	RequestTimeout time.Duration

	// ShutdownTimeout is how long in-flight requests get after SIGTERM.
	ShutdownTimeout time.Duration

	// MaxRequestBodyBytes caps request bodies. An unbounded body is a
	// memory-exhaustion primitive.
	MaxRequestBodyBytes int64

	// TrustedProxies lists reverse-proxy CIDRs whose X-Forwarded-For header is
	// believed. Empty means trust nothing — the correct default, because
	// trusting the header unconditionally lets a client pick its own IP and
	// walk past the rate limiter.
	TrustedProxies []string
}

// Address is the host:port the server binds.
func (s Server) Address() string {
	return net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
}

// Database holds the connection string and pool settings.
type Database struct {
	URL string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration

	// LogLevel is GORM's own query logger: silent, error, warn or info.
	LogLevel string
}

// Auth holds token and password-hashing settings.
type Auth struct {
	// JWTSecret signs access tokens. Validated at startup: non-empty, at least
	// MinJWTSecretLength characters, and not the .env.example placeholder.
	JWTSecret string
	JWTIssuer string
	JWTTTL    time.Duration

	// BcryptCost is the work factor. Each increment doubles hashing time.
	BcryptCost int

	// TokenSecret signs email-verification and password-reset tokens. When
	// empty it is derived from JWTSecret with a distinct label, so the two key
	// usages never share material. Set it explicitly to rotate them apart.
	TokenSecret string

	VerificationTokenTTL  time.Duration
	PasswordResetTokenTTL time.Duration
}

// TokenSecretIsDerived reports whether the verification-token key will be
// derived from JWTSecret rather than configured independently.
func (a Auth) TokenSecretIsDerived() bool {
	return a.TokenSecret == ""
}

// CORS holds the cross-origin policy.
type CORS struct {
	// AllowedOrigins is an exact-match allow-list. There is no wildcard
	// default, and "*" together with AllowCredentials is rejected at startup.
	AllowedOrigins   []string
	AllowCredentials bool
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	MaxAge           time.Duration
}

// AllowsOrigin reports whether origin is permitted. It is an exact,
// case-sensitive match — deliberately not a prefix or suffix test, which is how
// allow-lists usually end up accepting evil-example.com.
func (c CORS) AllowsOrigin(origin string) bool {
	var allowed string
	for _, allowed = range c.AllowedOrigins {
		if allowed == wildcardOrigin || allowed == origin {
			return true
		}
	}
	return false
}

// RateLimit holds the token-bucket settings.
type RateLimit struct {
	Enabled bool

	// RPS and Burst apply per client IP across every route.
	RPS   float64
	Burst int

	// AuthRPM and AuthBurst apply to the unauthenticated auth routes, where
	// credential stuffing is the thing being slowed down.
	AuthRPM   float64
	AuthBurst int
}

// Mail holds outbound-mail settings.
type Mail struct {
	// Driver is smtp (real delivery) or log (render to the logger and send
	// nothing). log is the default so a fresh clone runs with no mail server.
	Driver string

	FromAddress string
	FromName    string

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	// SMTPTLS is none, starttls or tls.
	SMTPTLS     string
	SMTPTimeout time.Duration
}

// UsesSMTP reports whether real delivery is configured.
func (m Mail) UsesSMTP() bool { return m.Driver == MailDriverSMTP }

// Frontend holds the client application's location.
type Frontend struct {
	// URL is the base for verification and password-reset links, so it must be
	// an origin you control.
	URL string
}

// Admin holds the optional seeded administrator.
type Admin struct {
	Username string
	Email    string
	Password string
}

// Complete reports whether all three values are present. When they are not, the
// seeder skips and logs a warning — a silent no-op here is indistinguishable
// from a successful boot with no way to log in.
func (a Admin) Complete() bool {
	return a.Username != "" && a.Email != "" && a.Password != ""
}

// Partial reports whether some but not all values are set, which is almost
// always a mistake worth warning about.
func (a Admin) Partial() bool {
	var anySet bool = a.Username != "" || a.Email != "" || a.Password != ""
	return anySet && !a.Complete()
}

// Google holds optional Google OAuth credentials.
type Google struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Enabled reports whether Google sign-in should be registered.
func (g Google) Enabled() bool {
	return g.ClientID != "" && g.ClientSecret != ""
}

// Sentry holds optional error tracking.
type Sentry struct {
	DSN              string
	Environment      string
	TracesSampleRate float64
}

// Enabled reports whether Sentry should be initialised.
func (s Sentry) Enabled() bool { return s.DSN != "" }

// Load reads the .env tiers, parses every key and validates the result.
//
// It returns a *ValidationError listing every problem found, so one restart
// surfaces all of them rather than the first.
func Load() (*Config, error) {
	var env Environment
	var loaded []string
	env, loaded = loadEnvFiles()

	var r *reader = &reader{}
	var cfg *Config = &Config{LoadedFiles: loaded}

	cfg.App = App{
		Env:     env,
		Name:    r.str("APP_NAME", defaultAppName),
		BaseURL: r.str("APP_BASE_URL", defaultAppBaseURL),
	}

	cfg.Log = Log{
		Level:  r.enum("LOG_LEVEL", defaultLogLevel, "trace", "debug", "info", "warn", "error", "fatal", "panic"),
		Format: r.enum("LOG_FORMAT", defaultLogFormat, LogFormatJSON, LogFormatConsole),
	}

	cfg.Server = Server{
		Host:                r.str("SERVICE_HOST", defaultServiceHost),
		Port:                r.integer("SERVICE_PORT", defaultServicePort),
		ReadTimeout:         r.duration("SERVER_READ_TIMEOUT", defaultReadTimeout),
		WriteTimeout:        r.duration("SERVER_WRITE_TIMEOUT", defaultWriteTimeout),
		IdleTimeout:         r.duration("SERVER_IDLE_TIMEOUT", defaultIdleTimeout),
		HeaderTimeout:       r.duration("SERVER_HEADER_TIMEOUT", defaultHeaderTimeout),
		RequestTimeout:      r.duration("REQUEST_TIMEOUT", defaultRequestTimeout),
		ShutdownTimeout:     r.duration("SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
		MaxRequestBodyBytes: r.integer64("MAX_REQUEST_BODY_BYTES", defaultMaxRequestBodyBytes),
		TrustedProxies:      r.list("TRUSTED_PROXIES", nil),
	}

	cfg.Database = Database{
		URL:             r.requiredStr("DATABASE_URL"),
		MaxOpenConns:    r.integer("DATABASE_MAX_OPEN_CONNS", defaultMaxOpenConns),
		MaxIdleConns:    r.integer("DATABASE_MAX_IDLE_CONNS", defaultMaxIdleConns),
		ConnMaxLifetime: r.duration("DATABASE_CONN_MAX_LIFETIME", defaultConnMaxLifetime),
		ConnMaxIdleTime: r.duration("DATABASE_CONN_MAX_IDLE_TIME", defaultConnMaxIdleTime),
		LogLevel:        r.enum("DATABASE_LOG_LEVEL", defaultDatabaseLogLevel, "silent", "error", "warn", "info"),
	}

	cfg.Auth = Auth{
		JWTSecret:             r.requiredStr("JWT_SECRET"),
		JWTIssuer:             r.str("JWT_ISSUER", cfg.App.Name),
		JWTTTL:                r.duration("JWT_TTL", defaultJWTTTL),
		BcryptCost:            r.integer("BCRYPT_COST", defaultBcryptCost),
		TokenSecret:           r.str("TOKEN_SECRET", ""),
		VerificationTokenTTL:  r.duration("VERIFICATION_TOKEN_TTL", defaultVerificationTokenTTL),
		PasswordResetTokenTTL: r.duration("PASSWORD_RESET_TOKEN_TTL", defaultPasswordResetTokenTTL),
	}

	cfg.CORS = CORS{
		AllowedOrigins:   r.list("CORS_ALLOWED_ORIGINS", nil),
		AllowCredentials: r.boolean("CORS_ALLOW_CREDENTIALS", defaultCORSAllowCredentials),
		AllowedMethods:   r.list("CORS_ALLOWED_METHODS", defaultCORSMethods()),
		AllowedHeaders:   r.list("CORS_ALLOWED_HEADERS", defaultCORSHeaders()),
		ExposedHeaders:   r.list("CORS_EXPOSED_HEADERS", defaultCORSExposedHeaders()),
		MaxAge:           r.duration("CORS_MAX_AGE", defaultCORSMaxAge),
	}

	cfg.RateLimit = RateLimit{
		Enabled:   r.boolean("RATE_LIMIT_ENABLED", defaultRateLimitEnabled),
		RPS:       r.float("RATE_LIMIT_RPS", defaultRateLimitRPS),
		Burst:     r.integer("RATE_LIMIT_BURST", defaultRateLimitBurst),
		AuthRPM:   r.float("AUTH_RATE_LIMIT_RPM", defaultAuthRateLimitRPM),
		AuthBurst: r.integer("AUTH_RATE_LIMIT_BURST", defaultAuthRateLimitBurst),
	}

	cfg.Mail = Mail{
		Driver:       r.enum("MAIL_DRIVER", defaultMailDriver, MailDriverLog, MailDriverSMTP),
		FromAddress:  r.str("MAIL_FROM_ADDRESS", defaultMailFromAddress),
		FromName:     r.str("MAIL_FROM_NAME", cfg.App.Name),
		SMTPHost:     r.str("SMTP_HOST", ""),
		SMTPPort:     r.integer("SMTP_PORT", defaultSMTPPort),
		SMTPUsername: r.str("SMTP_USERNAME", ""),
		SMTPPassword: r.str("SMTP_PASSWORD", ""),
		SMTPTLS:      r.enum("SMTP_TLS", defaultSMTPTLS, SMTPTLSNone, SMTPTLSStartTLS, SMTPTLSImplicit),
		SMTPTimeout:  r.duration("SMTP_TIMEOUT", defaultSMTPTimeout),
	}

	cfg.Frontend = Frontend{
		URL: r.str("FRONTEND_URL", defaultFrontendURL),
	}

	cfg.Admin = Admin{
		Username: r.str("ADMIN_USERNAME", ""),
		Email:    r.str("ADMIN_EMAIL", ""),
		Password: r.str("ADMIN_PASSWORD", ""),
	}

	cfg.Google = Google{
		ClientID:     r.str("GOOGLE_CLIENT_ID", ""),
		ClientSecret: r.str("GOOGLE_CLIENT_SECRET", ""),
		RedirectURL:  r.str("GOOGLE_REDIRECT_URL", ""),
	}

	cfg.Sentry = Sentry{
		DSN:              r.str("SENTRY_DSN", ""),
		Environment:      r.str("SENTRY_ENVIRONMENT", env.String()),
		TracesSampleRate: r.float("SENTRY_TRACES_SAMPLE_RATE", defaultSentryTracesSampleRate),
	}

	cfg.validate(r)
	cfg.keysRead = append([]string{goEnvKey}, r.keys...)

	if len(r.problems) > 0 {
		return nil, &ValidationError{Problems: r.problems}
	}
	return cfg, nil
}

// loadEnvFiles applies the tiers in order, later files overriding earlier ones:
//
//	.env  ->  .env.$GO_ENV  ->  .env.$GO_ENV.local
//
// godotenv.Overload is used rather than Load because overriding is the whole
// point of a tier: a value in .env.development.local must beat the same key in
// .env. The cost is that it also overwrites variables already present in the
// process environment, which is why an explicitly exported GO_ENV is captured
// first and restored afterwards — otherwise `GO_ENV=production ./server` could
// be silently downgraded by a stale GO_ENV line in a committed .env.
//
// It also means a test process that runs from the repository root has its
// database DSN replaced by the developer's own. See docs/TESTING.md.
func loadEnvFiles() (Environment, []string) {
	var explicitEnv string = strings.TrimSpace(osGetenv(goEnvKey))

	var loaded []string
	if applyEnvFile(baseEnvFile) {
		loaded = append(loaded, baseEnvFile)
	}

	// .env may itself set GO_ENV, but an explicit process-level value wins.
	var envName string = explicitEnv
	if envName == "" {
		envName = strings.TrimSpace(osGetenv(goEnvKey))
	}
	if envName == "" {
		envName = string(EnvDevelopment)
	}
	envName = strings.ToLower(envName)

	var tierFile string = baseEnvFile + "." + envName
	if applyEnvFile(tierFile) {
		loaded = append(loaded, tierFile)
	}

	var localFile string = tierFile + ".local"
	if applyEnvFile(localFile) {
		loaded = append(loaded, localFile)
	}

	// Restore the explicit choice: the tier files must not redefine which tier
	// is in effect.
	if explicitEnv != "" {
		osSetenv(goEnvKey, explicitEnv)
		envName = strings.ToLower(explicitEnv)
	}

	return Environment(envName), loaded
}

// applyEnvFile loads one tier, reporting whether it existed. A missing tier is
// normal — most deployments have only one or two.
func applyEnvFile(path string) bool {
	var err error = godotenv.Overload(path)
	return err == nil
}

// ValidationError reports every configuration problem found in one pass.
type ValidationError struct {
	Problems []string
}

// Error renders the problems as a single multi-line message, which is what the
// operator sees on a failed boot.
func (e *ValidationError) Error() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("invalid configuration (%d problem(s)):", len(e.Problems)))

	var problem string
	for _, problem = range e.Problems {
		b.WriteString("\n  - ")
		b.WriteString(problem)
	}
	b.WriteString("\n\nEvery key is documented in .env.example.")
	return b.String()
}

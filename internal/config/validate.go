// File: internal/config/validate.go

package config

import (
	"net"
	"net/mail"
	"net/url"
	"strings"
)

// validate applies every cross-key and range rule, appending to the same
// problem list the reader used so one boot reports everything at once.
//
// The bias throughout is to fail the boot rather than start with a setting that
// is quietly unsafe. A service that will not start gets fixed in minutes; one
// that starts with a forgeable signing key can run for months.
func (c *Config) validate(r *reader) {
	c.validateApp(r)
	c.validateServer(r)
	c.validateDatabase(r)
	c.validateAuth(r)
	c.validateCORS(r)
	c.validateRateLimit(r)
	c.validateMail(r)
	c.validateIntegrations(r)
}

func (c *Config) validateApp(r *reader) {
	switch c.App.Env {
	case EnvDevelopment, EnvTest, EnvStaging, EnvProduction:
	default:
		r.problem(goEnvKey,
			"must be one of development, test, staging, production, got %q "+
				"(a typo here silently loads no .env tier)", c.App.Env)
	}

	validateAbsoluteURL(r, "APP_BASE_URL", c.App.BaseURL)
	validateAbsoluteURL(r, "FRONTEND_URL", c.Frontend.URL)
}

func (c *Config) validateServer(r *reader) {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		r.problem("SERVICE_PORT", "must be between 1 and 65535, got %d", c.Server.Port)
	}

	// The timeout middleware must be able to write its 503 before the server
	// tears the connection down underneath it.
	if c.Server.RequestTimeout >= c.Server.WriteTimeout {
		r.problem("REQUEST_TIMEOUT",
			"must be shorter than SERVER_WRITE_TIMEOUT (%s), got %s — otherwise the "+
				"connection is closed before the timeout response can be sent",
			c.Server.WriteTimeout, c.Server.RequestTimeout)
	}

	if c.Server.MaxRequestBodyBytes <= 0 {
		r.problem("MAX_REQUEST_BODY_BYTES",
			"must be greater than zero, got %d (an unbounded body is a memory-exhaustion risk)",
			c.Server.MaxRequestBodyBytes)
	}

	var proxy string
	for _, proxy = range c.Server.TrustedProxies {
		if !isCIDRorIP(proxy) {
			r.problem("TRUSTED_PROXIES",
				"%q is not a valid CIDR block or IP address", proxy)
		}
	}
}

func (c *Config) validateDatabase(r *reader) {
	if c.Database.URL == "" {
		// requiredStr already reported it; do not report the same key twice.
		return
	}

	var parsed *url.URL
	var err error
	parsed, err = url.Parse(c.Database.URL)
	if err != nil {
		r.problem("DATABASE_URL", "is not a valid connection URL: %v", err)
		return
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		r.problem("DATABASE_URL",
			"must use the postgres:// or postgresql:// scheme, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		r.problem("DATABASE_URL", "is missing a host")
	}

	if c.Database.MaxOpenConns < 1 {
		r.problem("DATABASE_MAX_OPEN_CONNS", "must be at least 1, got %d", c.Database.MaxOpenConns)
	}
	if c.Database.MaxIdleConns < 0 {
		r.problem("DATABASE_MAX_IDLE_CONNS", "cannot be negative, got %d", c.Database.MaxIdleConns)
	}
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		r.problem("DATABASE_MAX_IDLE_CONNS",
			"cannot exceed DATABASE_MAX_OPEN_CONNS (%d), got %d",
			c.Database.MaxOpenConns, c.Database.MaxIdleConns)
	}
}

func (c *Config) validateAuth(r *reader) {
	validateSecret(r, "JWT_SECRET", c.Auth.JWTSecret, true)

	// TOKEN_SECRET is optional: empty means "derive from JWT_SECRET". But a set
	// value must be as strong as the one it replaces.
	if c.Auth.TokenSecret != "" {
		validateSecret(r, "TOKEN_SECRET", c.Auth.TokenSecret, false)
	}

	if c.Auth.BcryptCost < minBcryptCost || c.Auth.BcryptCost > maxBcryptCost {
		r.problem("BCRYPT_COST",
			"must be between %d and %d, got %d", minBcryptCost, maxBcryptCost, c.Auth.BcryptCost)
	}

	if c.Auth.JWTIssuer == "" {
		r.problem("JWT_ISSUER", "cannot be empty — it is verified on every request")
	}
}

// validateSecret enforces length and rejects the shipped placeholder. The value
// itself is never echoed into the problem list.
func validateSecret(r *reader, key string, value string, required bool) {
	if value == "" {
		if required {
			// requiredStr already reported the absence.
			return
		}
		return
	}

	if strings.Contains(strings.ToUpper(value), placeholderMarker) {
		r.problem(key,
			"is still the placeholder from .env.example — generate a real value "+
				"with: openssl rand -base64 48")
		return
	}

	if len(value) < MinJWTSecretLength {
		r.problem(key,
			"must be at least %d characters, got %d — generate one with: openssl rand -base64 48",
			MinJWTSecretLength, len(value))
	}
}

func (c *Config) validateCORS(r *reader) {
	var hasWildcard bool

	var origin string
	for _, origin = range c.CORS.AllowedOrigins {
		if origin == wildcardOrigin {
			hasWildcard = true
			continue
		}
		validateOrigin(r, "CORS_ALLOWED_ORIGINS", origin)
	}

	// The Fetch standard forbids this combination outright, and where a browser
	// tolerates it every authenticated endpoint becomes readable by any site.
	if hasWildcard && c.CORS.AllowCredentials {
		r.problem("CORS_ALLOWED_ORIGINS",
			"cannot be \"*\" while CORS_ALLOW_CREDENTIALS is true — list the exact "+
				"origins instead, or set CORS_ALLOW_CREDENTIALS=false")
	}

	if hasWildcard && len(c.CORS.AllowedOrigins) > 1 {
		r.problem("CORS_ALLOWED_ORIGINS",
			"mixes \"*\" with explicit origins, which is ambiguous — use one or the other")
	}
}

// validateOrigin enforces the shape the CORS spec expects: scheme, host, and
// nothing else. A trailing slash or a path is the usual reason an allow-list
// silently fails to match, because Origin headers never carry one.
func validateOrigin(r *reader, key string, origin string) {
	var parsed *url.URL
	var err error
	parsed, err = url.Parse(origin)
	if err != nil {
		r.problem(key, "%q is not a valid origin: %v", origin, err)
		return
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		r.problem(key, "%q must start with http:// or https://", origin)
		return
	}
	if parsed.Host == "" {
		r.problem(key, "%q is missing a host", origin)
		return
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		r.problem(key,
			"%q must be scheme://host[:port] with no path, query or trailing slash — "+
				"browsers never send one in the Origin header, so it would never match",
			origin)
	}
}

func (c *Config) validateRateLimit(r *reader) {
	if !c.RateLimit.Enabled {
		return
	}

	if c.RateLimit.RPS <= 0 {
		r.problem("RATE_LIMIT_RPS", "must be greater than zero when rate limiting is enabled, got %v", c.RateLimit.RPS)
	}
	if c.RateLimit.Burst < 1 {
		r.problem("RATE_LIMIT_BURST", "must be at least 1 when rate limiting is enabled, got %d", c.RateLimit.Burst)
	}
	if c.RateLimit.AuthRPM <= 0 {
		r.problem("AUTH_RATE_LIMIT_RPM", "must be greater than zero when rate limiting is enabled, got %v", c.RateLimit.AuthRPM)
	}
	if c.RateLimit.AuthBurst < 1 {
		r.problem("AUTH_RATE_LIMIT_BURST", "must be at least 1 when rate limiting is enabled, got %d", c.RateLimit.AuthBurst)
	}
}

func (c *Config) validateMail(r *reader) {
	if c.Mail.FromAddress != "" {
		var _, err = mail.ParseAddress(c.Mail.FromAddress)
		if err != nil {
			r.problem("MAIL_FROM_ADDRESS", "is not a valid email address: %v", err)
		}
	}

	if !c.Mail.UsesSMTP() {
		return
	}

	if c.Mail.SMTPHost == "" {
		r.problem("SMTP_HOST", "is required when MAIL_DRIVER=smtp")
	}
	if c.Mail.SMTPPort < 1 || c.Mail.SMTPPort > 65535 {
		r.problem("SMTP_PORT", "must be between 1 and 65535, got %d", c.Mail.SMTPPort)
	}
}

func (c *Config) validateIntegrations(r *reader) {
	// Google OAuth: the credential pair is all-or-nothing, because half a
	// credential fails at the callback rather than at boot — a much worse place
	// to discover it.
	//
	// GOOGLE_REDIRECT_URL is deliberately *not* part of that rule. .env.example
	// ships it prefilled to show the expected route shape, which is useful
	// documentation and harmless while the provider is disabled. It only
	// becomes required once the credentials are actually set.
	var hasID bool = c.Google.ClientID != ""
	var hasSecret bool = c.Google.ClientSecret != ""

	if hasID != hasSecret {
		r.problem("GOOGLE_CLIENT_ID",
			"Google OAuth is half-configured — set GOOGLE_CLIENT_ID and "+
				"GOOGLE_CLIENT_SECRET together, or leave both empty to disable it")
	}
	if c.Google.Enabled() && c.Google.RedirectURL == "" {
		r.problem("GOOGLE_REDIRECT_URL",
			"is required when Google OAuth credentials are set")
	}
	if c.Google.RedirectURL != "" {
		validateAbsoluteURL(r, "GOOGLE_REDIRECT_URL", c.Google.RedirectURL)
	}

	if c.Sentry.TracesSampleRate < 0 || c.Sentry.TracesSampleRate > 1 {
		r.problem("SENTRY_TRACES_SAMPLE_RATE",
			"must be between 0.0 and 1.0, got %v", c.Sentry.TracesSampleRate)
	}
}

// validateAbsoluteURL checks for a scheme and a host. Links built from a
// relative or malformed base silently produce unreachable verification and
// password-reset emails.
func validateAbsoluteURL(r *reader, key string, value string) {
	if value == "" {
		return
	}

	var parsed *url.URL
	var err error
	parsed, err = url.Parse(value)
	if err != nil {
		r.problem(key, "is not a valid URL: %v", err)
		return
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		r.problem(key, "must be an absolute URL such as https://example.com, got %q", value)
	}
}

// isCIDRorIP accepts both notations, since a single proxy is naturally written
// as a bare address rather than a /32.
func isCIDRorIP(value string) bool {
	var _, _, cidrErr = net.ParseCIDR(value)
	if cidrErr == nil {
		return true
	}
	return net.ParseIP(value) != nil
}

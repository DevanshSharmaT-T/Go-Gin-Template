// File: internal/config/config_test.go

package config

import (
	"errors"
	"strings"
	"testing"
)

// validEnv is the smallest environment that produces a usable Config: only
// DATABASE_URL and JWT_SECRET have no default. If this stops being true, the
// "clone and run" promise in README.md has quietly broken.
func validEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:5432/app?sslmode=disable")
	t.Setenv("JWT_SECRET", strings.Repeat("k", MinJWTSecretLength))
}

// isolate runs the test in an empty directory so no .env tier from the
// repository root is discovered. godotenv.Overload would otherwise overwrite
// the values t.Setenv just established — the same trap documented for the test
// harness in docs/TESTING.md.
func isolate(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
}

func problems(t *testing.T, err error) []string {
	t.Helper()
	if err == nil {
		t.Fatal("want a configuration error, got nil")
	}
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want *ValidationError, got %T: %v", err, err)
	}
	return verr.Problems
}

func assertMentions(t *testing.T, found []string, key string) {
	t.Helper()
	for _, problem := range found {
		if strings.HasPrefix(problem, key+":") {
			return
		}
	}
	t.Errorf("want a problem reported against %s, got:\n  %s", key, strings.Join(found, "\n  "))
}

func TestLoad_MinimalEnvironmentIsValid(t *testing.T) {
	isolate(t)
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("a minimal environment must load: %v", err)
	}

	if cfg.App.Env != EnvDevelopment {
		t.Errorf("GO_ENV should default to development, got %q", cfg.App.Env)
	}
	if cfg.Server.Address() != "127.0.0.1:8080" {
		t.Errorf("want 127.0.0.1:8080, got %q", cfg.Server.Address())
	}
	if cfg.Mail.Driver != MailDriverLog {
		t.Errorf("mail must default to the log driver so a clone runs without a mail server, got %q", cfg.Mail.Driver)
	}
	if !cfg.Auth.TokenSecretIsDerived() {
		t.Error("an unset TOKEN_SECRET should be derived from JWT_SECRET")
	}
	if cfg.Sentry.Enabled() || cfg.Google.Enabled() || cfg.Admin.Complete() {
		t.Error("optional integrations must be off by default")
	}
}

func TestLoad_RejectsMissingRequiredKeys(t *testing.T) {
	isolate(t)

	found := problems(t, mustFail(t))

	assertMentions(t, found, "DATABASE_URL")
	assertMentions(t, found, "JWT_SECRET")
}

// mustFail loads with no required keys set and returns the error.
func mustFail(t *testing.T) error {
	t.Helper()
	_, err := Load()
	return err
}

func TestLoad_ReportsEveryProblemAtOnce(t *testing.T) {
	// One restart should surface every fault, not the first one.
	isolate(t)
	t.Setenv("DATABASE_URL", "mysql://localhost/app")
	t.Setenv("JWT_SECRET", "short")
	t.Setenv("BCRYPT_COST", "3")
	t.Setenv("SERVICE_PORT", "70000")

	found := problems(t, mustFail(t))

	assertMentions(t, found, "DATABASE_URL")
	assertMentions(t, found, "JWT_SECRET")
	assertMentions(t, found, "BCRYPT_COST")
	assertMentions(t, found, "SERVICE_PORT")

	if len(found) < 4 {
		t.Errorf("want at least 4 problems reported together, got %d:\n  %s",
			len(found), strings.Join(found, "\n  "))
	}
}

func TestLoad_RejectsPlaceholderJWTSecret(t *testing.T) {
	// The .env.example placeholder is longer than the minimum, so a length
	// check alone would let a copied example file boot with a public key.
	isolate(t)
	validEnv(t)
	t.Setenv("JWT_SECRET", "CHANGE_ME_generate_with_openssl_rand_base64_48")

	found := problems(t, mustFail(t))
	assertMentions(t, found, "JWT_SECRET")

	if !strings.Contains(strings.Join(found, "\n"), "placeholder") {
		t.Errorf("the message should say it is the placeholder, got:\n  %s", strings.Join(found, "\n  "))
	}
}

func TestLoad_RejectsShortJWTSecret(t *testing.T) {
	isolate(t)
	validEnv(t)
	t.Setenv("JWT_SECRET", strings.Repeat("k", MinJWTSecretLength-1))

	assertMentions(t, problems(t, mustFail(t)), "JWT_SECRET")
}

func TestLoad_NeverEchoesASecretIntoTheProblemList(t *testing.T) {
	// Problem lists get pasted into issues and screenshots.
	isolate(t)
	validEnv(t)
	const secret = "too-short-but-a-real-secret"
	t.Setenv("JWT_SECRET", secret)

	joined := strings.Join(problems(t, mustFail(t)), "\n")
	if strings.Contains(joined, secret) {
		t.Errorf("the secret value leaked into the error message:\n%s", joined)
	}
}

func TestLoad_RejectsCORSWildcardWithCredentials(t *testing.T) {
	isolate(t)
	validEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "*")
	t.Setenv("CORS_ALLOW_CREDENTIALS", "true")

	assertMentions(t, problems(t, mustFail(t)), "CORS_ALLOWED_ORIGINS")
}

func TestLoad_AllowsCORSWildcardWithoutCredentials(t *testing.T) {
	isolate(t)
	validEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "*")
	t.Setenv("CORS_ALLOW_CREDENTIALS", "false")

	if _, err := Load(); err != nil {
		t.Fatalf("a wildcard without credentials is legitimate: %v", err)
	}
}

func TestLoad_RejectsOriginWithTrailingSlash(t *testing.T) {
	// Origin headers never carry a path, so this would never match at runtime.
	isolate(t)
	validEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000/")

	assertMentions(t, problems(t, mustFail(t)), "CORS_ALLOWED_ORIGINS")
}

func TestLoad_RejectsRequestTimeoutAtOrAboveWriteTimeout(t *testing.T) {
	isolate(t)
	validEnv(t)
	t.Setenv("SERVER_WRITE_TIMEOUT", "10s")
	t.Setenv("REQUEST_TIMEOUT", "10s")

	assertMentions(t, problems(t, mustFail(t)), "REQUEST_TIMEOUT")
}

func TestLoad_RejectsIdleConnectionsAboveOpenConnections(t *testing.T) {
	isolate(t)
	validEnv(t)
	t.Setenv("DATABASE_MAX_OPEN_CONNS", "5")
	t.Setenv("DATABASE_MAX_IDLE_CONNS", "10")

	assertMentions(t, problems(t, mustFail(t)), "DATABASE_MAX_IDLE_CONNS")
}

func TestLoad_RejectsPartialGoogleOAuth(t *testing.T) {
	// A half-configured provider fails at the callback instead of at boot.
	isolate(t)
	validEnv(t)
	t.Setenv("GOOGLE_CLIENT_ID", "client-id")

	assertMentions(t, problems(t, mustFail(t)), "GOOGLE_CLIENT_ID")
}

func TestLoad_RejectsInvalidTrustedProxy(t *testing.T) {
	isolate(t)
	validEnv(t)
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8,not-an-address")

	assertMentions(t, problems(t, mustFail(t)), "TRUSTED_PROXIES")
}

func TestLoad_RejectsUnknownEnumValues(t *testing.T) {
	isolate(t)
	validEnv(t)
	t.Setenv("LOG_FORMAT", "xml")
	t.Setenv("MAIL_DRIVER", "carrier-pigeon")
	t.Setenv("GO_ENV", "prod")

	found := problems(t, mustFail(t))
	assertMentions(t, found, "LOG_FORMAT")
	assertMentions(t, found, "MAIL_DRIVER")
	assertMentions(t, found, "GO_ENV")
}

func TestLoad_SMTPDriverRequiresAHost(t *testing.T) {
	isolate(t)
	validEnv(t)
	t.Setenv("MAIL_DRIVER", "smtp")

	assertMentions(t, problems(t, mustFail(t)), "SMTP_HOST")
}

func TestLoad_ParsesDurationsAndLists(t *testing.T) {
	isolate(t)
	validEnv(t)
	t.Setenv("JWT_TTL", "45m")
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000, https://app.example.com ,")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Auth.JWTTTL.Minutes() != 45 {
		t.Errorf("want 45m, got %s", cfg.Auth.JWTTTL)
	}
	if len(cfg.CORS.AllowedOrigins) != 2 {
		t.Fatalf("want 2 origins after trimming blanks, got %v", cfg.CORS.AllowedOrigins)
	}
	if !cfg.CORS.AllowsOrigin("https://app.example.com") {
		t.Error("want the whitespace-padded origin to be usable")
	}
}

func TestCORS_AllowsOrigin_IsExactMatch(t *testing.T) {
	// A prefix or suffix test is how allow-lists end up accepting
	// evil-example.com and example.com.attacker.test.
	cors := CORS{AllowedOrigins: []string{"https://example.com"}}

	if !cors.AllowsOrigin("https://example.com") {
		t.Error("the listed origin must be allowed")
	}
	for _, origin := range []string{
		"https://example.com.attacker.test",
		"https://evil-example.com",
		"http://example.com",
		"https://example.com/",
		"https://sub.example.com",
	} {
		if cors.AllowsOrigin(origin) {
			t.Errorf("%q must not match", origin)
		}
	}
}

func TestAdmin_PartialIsDistinguishableFromUnset(t *testing.T) {
	if (Admin{}).Partial() {
		t.Error("an entirely unset admin is not a partial configuration")
	}
	if !(Admin{Username: "admin"}).Partial() {
		t.Error("one field set out of three is a partial configuration")
	}
	if (Admin{Username: "a", Email: "e", Password: "p"}).Partial() {
		t.Error("a complete admin is not partial")
	}
}

func TestValidationError_ListsEveryProblem(t *testing.T) {
	err := &ValidationError{Problems: []string{"A: broken", "B: also broken"}}

	message := err.Error()
	for _, want := range []string{"2 problem(s)", "A: broken", "B: also broken", ".env.example"} {
		if !strings.Contains(message, want) {
			t.Errorf("want %q in the message, got:\n%s", want, message)
		}
	}
}

func TestLoad_GoogleRedirectURLAloneIsHarmless(t *testing.T) {
	// .env.example ships the redirect URL prefilled as documentation. With no
	// credentials set the provider is simply disabled, and the file must still
	// validate — see TestEnvExample_IsUsableAsWritten.
	isolate(t)
	validEnv(t)
	t.Setenv("GOOGLE_REDIRECT_URL", "http://localhost:8080/api/auth/google/callback")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("a redirect URL without credentials must not fail the boot: %v", err)
	}
	if cfg.Google.Enabled() {
		t.Error("Google sign-in must stay disabled without credentials")
	}
}

func TestLoad_EnabledGoogleRequiresARedirectURL(t *testing.T) {
	isolate(t)
	validEnv(t)
	t.Setenv("GOOGLE_CLIENT_ID", "client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "client-secret")

	assertMentions(t, problems(t, mustFail(t)), "GOOGLE_REDIRECT_URL")
}

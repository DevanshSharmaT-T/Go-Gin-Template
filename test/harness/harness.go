// File: test/harness/harness.go

// Package harness boots the real application for tests.
//
// The point of a harness is that an integration test exercises the same graph
// production does. Nothing here re-implements wiring: App composes the same
// fx modules the entry point composes, so a provider that would fail to resolve
// at startup fails the test too — which is the only way wiring mistakes get
// caught, given that fx resolves by reflection at run time.
//
// It grows with the template. Today it is App; later phases add StartServer for
// the e2e suite, fixtures and a truncation helper.
package harness

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/app"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
)

// databaseURLEnv names the DSN the DB-backed suites connect to. It is separate
// from DATABASE_URL so that running the tests can never mean running them
// against a development database by accident.
const databaseURLEnv = "TEST_DATABASE_URL"

// Migrations and seeding run inside fx's start timeout, and fx's default of 15
// seconds is comfortable for a warm database and not for a container that
// started a moment ago. Raising it here trades a slower failure for a test
// suite that is not flaky on a cold machine.
//
// An entry point needs the same fx.StartTimeout for the same reason.
const (
	startTimeout = 60 * time.Second
	stopTimeout  = 30 * time.Second
)

// testJWTSecret satisfies the startup validation — at least 32 characters and
// not the .env.example placeholder. It signs nothing that outlives the test.
const testJWTSecret = "harness-only-signing-key-do-not-use-in-production"

// RequireDatabaseURL returns the test DSN, skipping the test when it is unset.
//
// Skipping rather than failing is deliberate: `go test ./...` has to pass on a
// machine with no PostgreSQL, or the fast suite stops being run at all.
func RequireDatabaseURL(t *testing.T) string {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv(databaseURLEnv))
	if dsn == "" {
		t.Skipf("%s is not set — run `make test-db-up` to start a disposable database", databaseURLEnv)
	}
	return dsn
}

// App boots the real dependency-injection graph against the test database and
// populates targets, each of which must be a pointer to something the graph
// provides:
//
//	var db *gorm.DB
//	harness.App(t, []any{&db})
//
// Extra fx options are appended after the application's own, so they can add
// providers — a test-local model or seeder registered through the same value
// group tags a module would use — or replace one with fx.Decorate, which is how
// you boot a real graph with exactly one dependency faked.
//
// The application is stopped during test cleanup, so nothing leaks a connection
// pool into the next test.
func App(t *testing.T, targets []any, opts ...fx.Option) *fx.App {
	t.Helper()

	dsn := RequireDatabaseURL(t)
	isolate(t, dsn)

	options := []fx.Option{
		fx.StartTimeout(startTimeout),
		fx.StopTimeout(stopTimeout),
		// Quiet by default; fx's start error already names the type it could
		// not resolve. Pass fx.WithLogger(...) in opts to see the whole graph.
		fx.NopLogger,

		// The same list cmd/api composes. Not a subset of it: a test that
		// boots a graph the entry point does not is testing something nobody
		// runs.
		app.Modules,
	}
	options = append(options, opts...)
	if len(targets) > 0 {
		options = append(options, fx.Populate(targets...))
	}

	app := fx.New(options...)

	startCtx, cancelStart := context.WithTimeout(context.Background(), startTimeout)
	defer cancelStart()

	if err := app.Start(startCtx); err != nil {
		t.Fatalf("harness: the application failed to start: %v", err)
	}

	t.Cleanup(func() {
		stopCtx, cancelStop := context.WithTimeout(context.Background(), stopTimeout)
		defer cancelStop()

		if err := app.Stop(stopCtx); err != nil {
			t.Errorf("harness: the application failed to stop cleanly: %v", err)
		}
	})

	return app
}

// isolate points the process at the test database and moves it somewhere the
// .env tier files cannot be found.
//
// The t.Chdir is the part that matters, and it is not tidiness. Configuration
// is loaded with godotenv.Overload, which *overwrites* process environment
// variables with the contents of .env, .env.$GO_ENV and .env.$GO_ENV.local.
// Run from the repository root and the DATABASE_URL set two lines above is
// silently replaced by the developer's own — and the suite migrates, seeds and
// writes into their development database. Running from an empty temporary
// directory means there is no tier file to find. Mail templates are embedded
// with //go:embed, so nothing needs the repository root at run time.
//
// If you add an entry point to this package, preserve it.
func isolate(t *testing.T, dsn string) {
	t.Helper()

	t.Setenv("GO_ENV", string(config.EnvTest))
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("JWT_SECRET", testJWTSecret)

	// Boot logging is noise in a passing test and rarely the reason for a
	// failing one; a test that wants it can override either key.
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("DATABASE_LOG_LEVEL", "warn")

	t.Chdir(t.TempDir())
}

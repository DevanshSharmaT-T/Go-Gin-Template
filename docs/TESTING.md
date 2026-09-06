# Testing

Three layers, each proving something the others cannot, and a harness that lets a test reach the
**real** application rather than a re-declared copy of it.

- [One constraint up front](#one-constraint-up-front)
- [Layout](#layout)
- [The three layers](#the-three-layers)
- [Linkage: reaching the application from a test](#linkage-reaching-the-application-from-a-test)
- [Running the suites](#running-the-suites)
- [The trap the harness works around](#the-trap-the-harness-works-around)
- [Conventions](#conventions)
- [Adding tests for a new module](#adding-tests-for-a-new-module)

---

## One constraint up front

Go **requires** a package's own tests to live in the same directory as the code: only a file in
`package foo` (or `package foo_test` in that directory) can see unexported identifiers. A test tree
fully detached from the source is therefore impossible for white-box tests — that is a property of
the language, not a choice this template made.

What *is* possible, and what this template does, is the layout the Go community converged on:
white-box tests stay beside their package; everything else — black-box unit tests, integration
tests, end-to-end tests, fixtures, mocks and shared harness code — lives in its own tree.

---

## Layout

```
test/
  harness/       linkage layer: boots the real app for tests
  fixtures/      builders for valid domain entities
  unit/          black-box unit tests — mocks, no I/O, always run
  integration/   real fx graph + real PostgreSQL       [build tag: integration]
  e2e/           the compiled server, driven over HTTP  [build tag: e2e]
  testdata/      static fixtures and golden files
internal/mocks/  generated mocks, one package per repository interface
```

Package-local tests — for example `internal/shared/crypt/encryption_test.go`, which exercises
unexported token encoding — stay next to their source.

`testdata/` is a name Go treats specially: the toolchain ignores it when resolving packages, so
anything in there is data, never code.

---

## The three layers

| Layer | Database | Build tag | Runs on `make test` | What it proves |
|---|---|---|---|---|
| `test/unit` | no | none | yes | service logic, against mocked repositories |
| `test/integration` | yes | `integration` | no | GORM mappings, migrations, seeders, the real DI graph |
| `test/e2e` | yes | `e2e` | no | routing, middleware, auth, the real HTTP contract |

Build tags keep the default `go test ./...` fast and dependency-free. That is not a convenience —
it is what makes the inner development loop and the default CI job runnable on any machine with no
PostgreSQL, which is the difference between a test suite people run and one they skip.

**What belongs where.** If the answer to "would this have caught the bug?" is a business rule, it is
a unit test. If it is a column type, an index, a migration or a seeder, it is an integration test.
If it is a status code, a middleware ordering or a permission gate, it is e2e. Writing a unit test
with three layers of mocks to check a route usually means you wanted an e2e test.

---

## Linkage: reaching the application from a test

### 1. Mocks — for unit tests

Every repository is an interface declared in its module's `domain` package. Those interfaces are the
seams. `internal/mocks/` holds a generated mock per interface, produced by
[`go.uber.org/mock`](https://github.com/uber-go/mock):

```go
ctrl := gomock.NewController(t)
repo := mockusers.NewMockUserRepository(ctrl)
repo.EXPECT().
    FindByEmail(gomock.Any(), "user@example.com").
    Return(nil, nil)                       // nil, nil == not found

svc := service.NewUserService(repo, crypt, mailer)   // real service, fake persistence
```

`nil, nil` is the "no such row" signal for a *lookup*; a method that operates on a specific row
returns a `NOT_FOUND` `AppError` instead. See
[Absence is not always an error](ARCHITECTURE.md#absence-is-not-always-an-error).

Regenerate after changing an interface:

```bash
make mocks
```

The generated mock failing to compile is the *point*: adding a method to a repository interface
should break every stale mock immediately rather than let tests keep exercising an old seam.

### 2. `harness.App` — for integration tests

`harness.App` boots the **real** dependency-injection graph — literally `internal/app.Modules`, the
same value the entry point composes — against the test database, and populates whatever you ask for.
Migrations, seeders and the registry warm-up all run, exactly as in production:

```go
var repo domain.UserRepository
harness.App(t, []any{&repo})    // wired GORM adapter, schema already migrated
```

Ask for any service or repository the graph provides. Extra `fx.Option` values can be passed to
decorate or replace providers, so you can boot a real graph with exactly one dependency mocked —
a real database with a fake mailer, for instance.

This is the layer that catches wiring mistakes. Because
[fx failures happen at startup rather than at compile time](ARCHITECTURE.md#dependency-injection-rules-that-fail-at-startup-not-at-compile-time),
a test that builds the real graph is the only thing that catches a missing `group:"models"` stanza
before deployment does.

### 3. `harness.StartServer` — for end-to-end tests

The route table is built in `package main`, which cannot be imported. Rather than re-declaring
routes in the test suite — where they would silently drift from production — `harness.StartServer`
compiles `cmd/api`, runs it on a free port against the test database, waits for it to answer, and
returns a base URL and an HTTP client:

```go
srv := harness.StartServer(t)
resp, err := srv.Client.Get(srv.URL("/api/health"))
```

The process, its port and its temporary files are cleaned up automatically. `srv.Logs()` returns the
server's output, which is where startup failures surface — check it first when an e2e test fails
with a connection error rather than an assertion.

### 4. Fixtures

`test/fixtures` builds valid entities so a test states only what it actually cares about:

```go
user := fixtures.NewUser()                                  // valid, active, lowest privilege
admin := fixtures.NewUser(fixtures.WithRole(fixtures.RoleAdmin))
locked := fixtures.NewUser(fixtures.WithStatus(domain.StatusSuspended))
```

Usernames and emails are generated to satisfy the production validation rules and to be unique per
process, so fixtures never collide on unique indexes when suites run in parallel.

---

## Running the suites

```bash
make test              # unit + package tests, race detector, no database
make test-cover        # the same, with a coverage profile
make test-db-up        # disposable PostgreSQL on port 55433
make test-integration
make test-e2e
make test-all
make test-db-down      # tear it down
```

Run a single test:

```bash
go test ./internal/shared/crypt/... -run TestVerificationToken_RoundTrip -v
```

The DB-backed suites read `TEST_DATABASE_URL` and **skip** when it is unset, so they never fail
merely because a machine has no database:

```bash
export TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:55433/app_test?sslmode=disable'
```

> **Never point `TEST_DATABASE_URL` at a database you care about.** The suites run real migrations
> and write real rows. `make test-db-up` starts a throwaway container for exactly this reason.

---

## The trap the harness works around

Configuration is loaded with `godotenv.Overload`, which **overwrites** process environment variables
with the contents of the `.env` tier files. That is deliberate — it is what makes the tiers layer
correctly — but it means a test process that runs from the repository root has the
`TEST_DATABASE_URL`-derived DSN it just set *silently replaced* by the developer's own
`DATABASE_URL`, and writes test rows into the development database.

Both `harness.App` and `harness.StartServer` therefore run from an **empty temporary directory**, so
no `.env` file is discovered and the values they set actually take effect. Mail templates are
embedded with `//go:embed`, so nothing needs the repository root at runtime.

**If you add a harness entry point, preserve this.** It is the kind of bug that does no visible
damage until the day it drops someone's local data.

---

## Conventions

- **Test code is idiomatic Go and uses `:=`.** The
  [`var`-with-explicit-type mandate](ARCHITECTURE.md#declarations-use-var-with-an-explicit-type)
  applies to production code under `internal/` and `cmd/` only. `test/harness` and `test/fixtures`
  are test infrastructure and follow the same relaxed rule.

- **Name the test for the behaviour asserted, not the method called.**
  `TestUserService_Register_AssignsDefaultRole` says what broke when it fails;
  `TestRegister` does not.

- **Assert on the error *type*, not the message string.** Messages are wording; types are contract.

  ```go
  var appErr *errors.AppError
  if !errors.As(err, &appErr) || appErr.ToHTTPStatus() != http.StatusConflict {
      t.Fatalf("want CONFLICT, got %v", err)
  }
  ```

- **Every security fix gets a permanent regression test.** A fix without one is a fix that gets
  reverted by a well-meaning refactor. Test the corrected behaviour at the lowest layer that can see
  it, and again at e2e if it is reachable over HTTP — for example, the token codec is covered by a
  unit test on the encoder and by an e2e test that a tampered token is rejected.

- **`t.Parallel()` where the test allows it**, and keep the race detector on. `make test` runs
  `-race` by default; a data race that only appears under load is not something you want to find in
  production.

- **No sleeps.** Wait on a condition with a deadline. `harness.StartServer` polls for readiness
  rather than sleeping, and tests should follow that pattern.

---

## Adding tests for a new module

1. `make mocks` after adding the repository interface.
2. Service logic → `test/unit`, using the generated mock.
3. Anything touching SQL, migrations or seeders → `test/integration` via `harness.App`.
4. New routes, middleware or auth behaviour → `test/e2e` via `harness.StartServer`.
5. Add a fixture builder to `test/fixtures` if other tests will need the entity.

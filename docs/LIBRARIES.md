# Libraries

Every third-party dependency in this template, what it does, and **why it is here rather than an
alternative**. If you are evaluating the template, this is the file that tells you what you are
signing up for.

The set is deliberately small. Go's standard library already covers HTTP, JSON, crypto primitives,
templating and testing well; a dependency earns its place only when the stdlib version would cost
meaningful hand-written code that has nothing to do with your product.

- [HTTP](#http)
- [Persistence](#persistence)
- [Dependency injection](#dependency-injection)
- [Auth & crypto](#auth--crypto)
- [Identity](#identity)
- [Configuration](#configuration)
- [Logging & observability](#logging--observability)
- [API documentation](#api-documentation)
- [Testing](#testing)
- [Tooling (not libraries)](#tooling-not-libraries)
- [Where each library is used](#where-each-library-is-used)
- [Deliberately absent](#deliberately-absent)

---

## HTTP

### `github.com/gin-gonic/gin`

<https://github.com/gin-gonic/gin> · MIT

The HTTP framework: routing, route groups, middleware chaining, request binding and response
rendering. Gin is a thin layer over `net/http` — a `gin.HandlerFunc` receives a `*gin.Context` that
wraps the standard `http.Request` and `http.ResponseWriter`, so anything from the standard library
ecosystem still works.

**Why Gin.** Route groups map exactly onto the three-tier route structure this template uses
(public → authenticated → permission-gated), and its middleware model — `c.Next()`, `c.Abort()`,
`c.Set()`/`c.Get()` for request-scoped values — is what the auth and RBAC layers are built on.
It is the most widely deployed Go web framework, so hiring, examples and Stack Overflow answers all
work in your favour.

**Why not `net/http` + `chi`?** Go 1.22's `http.ServeMux` and chi are both excellent and lighter.
The cost of dropping Gin is that you hand-write binding, validation wiring, and error rendering for
every handler. This template chooses the framework because a *starter* should have those decisions
already made; if you want to strip it out later, the `api/` layer is the only layer that imports it.

### `github.com/go-playground/validator/v10`

<https://github.com/go-playground/validator> · MIT

Struct-tag validation: `binding:"required,email,min=8"`. Gin embeds it, so `c.ShouldBindJSON(&dto)`
runs both decoding and validation in one call and returns a `validator.ValidationErrors` you can
translate into field-level messages.

**Why it is called out separately.** Because you will use it directly. This template converts
validator errors into the project's own `*AppError` with a `VALIDATION` type rather than returning
the library's raw message, and registers custom validators (for example the username allow-list) on
the shared validator instance. Knowing that the engine underneath Gin's `binding` tag is this
library is what makes that possible.

---

## Persistence

### `gorm.io/gorm`

<https://gorm.io> · MIT

The ORM. Model structs carry `gorm:"..."` tags that describe columns, indexes and constraints;
`AutoMigrate` reconciles the database with those tags; associations, hooks, soft deletes,
transactions and a context-aware API come with it.

**Why GORM.** The schema lives in one place — the Go struct — which removes the class of bug where
a model field and a hand-written `CREATE TABLE` drift apart. For CRUD-shaped domains it is
substantially less code than the alternatives, and its `Session`/`WithContext` support means request
cancellation and timeouts propagate into the driver.

**Why not `sqlc` or plain `database/sql`?** This is the most contentious choice in the template, so
it is worth stating plainly. `sqlc` generates type-safe Go from real SQL and gives you exact control
over every query and its plan; it is arguably the better tool for query-heavy, performance-sensitive
services. GORM is chosen here because a template's value is in how fast you can add the *tenth*
entity, and GORM's cost per entity is a struct plus a repository. The mitigation for GORM's usual
downsides is architectural: **all GORM code is confined to `infra/`**, behind a repository interface
declared in `domain/`. When one query needs hand-written SQL, `db.Raw(...).Scan(...)` inside that
same adapter is a local change; when a whole module outgrows the ORM, its adapter can be rewritten
against `sqlc` without any other layer noticing.

### `gorm.io/driver/postgres`

<https://github.com/go-gorm/postgres> · MIT

GORM's PostgreSQL dialect — it translates the model tags into Postgres DDL and types, and knows how
to build `ON CONFLICT` upserts, `RETURNING` clauses and array/JSONB handling. It is what makes
`uuid` primary keys and native `ENUM` state columns work rather than being emulated in application
code.

### `github.com/jackc/pgx/v5`

<https://github.com/jackc/pgx> · MIT

The actual PostgreSQL driver, used underneath the GORM dialect. pgx speaks the binary wire protocol,
which is faster and type-safer than the text protocol, and it brings correct handling of Postgres
types the `lib/pq` driver treats as strings.

**Why it matters to you even though GORM hides it.** `DATABASE_URL` is parsed by pgx, so pgx's DSN
options (`sslmode`, `pool_max_conns`, `application_name`, `statement_cache_mode`) are the ones that
apply. Driver-level errors surface as `*pgconn.PgError` with a `Code` field, which is how the
repository layer distinguishes a unique-constraint violation (`23505` → a `CONFLICT` `AppError`)
from a genuine internal failure — without string-matching error text.

---

## Dependency injection

### `go.uber.org/fx` (and `go.uber.org/dig`)

<https://uber-go.github.io/fx/> · MIT

`fx` is an application framework built on the `dig` reflection-based DI container. You register
constructors with `fx.Provide`; fx reads their parameter and return types, works out the
instantiation order, and builds only what is actually reachable. `fx.Invoke` forces the graph to
build. `fx.Lifecycle` gives you `OnStart`/`OnStop` hooks, which is where the database connection,
migrations, seeders and the HTTP server are started and shut down in order.

**Why DI at all, instead of hand-wiring in `main()`?** With five modules and four layers each, a
hand-written `main()` becomes a hundred lines of ordering-sensitive constructor calls that everyone
has to edit and that produce merge conflicts on every feature branch. With fx, adding a module means
adding `fx.Provide` lines to *that module's own* `module.go` — the composition root never grows.
You also get two things that are tedious to hand-roll: ordered, error-checked startup/shutdown
hooks, and the ability for a test to boot the **real** graph and swap one provider (see
[`TESTING.md`](TESTING.md)).

**Why not `google/wire`?** Wire is compile-time DI: it generates the wiring code, so mistakes are
build errors rather than startup panics, and there is no reflection at runtime. That is a real
advantage and it is the right call for some teams. fx is chosen here for the lifecycle management
and value groups (`group:"models"`, `group:"seeders"`) that let a module register its migrations and
seeders without any central file listing them — Wire has no equivalent, and the codegen step is one
more thing to keep in sync. The trade-off you accept is that **wiring errors appear at startup, not
at compile time**; [`ARCHITECTURE.md`](ARCHITECTURE.md#dependency-injection-rules-that-fail-at-startup-not-at-compile-time)
documents the four failure modes this causes and how to recognise them.

---

## Auth & crypto

### `github.com/golang-jwt/jwt/v5`

<https://github.com/golang-jwt/jwt> · MIT

Signs and verifies JSON Web Tokens. The template issues HS256 access tokens carrying the user ID,
role, hierarchy level and a flattened permission map.

**Why v5 specifically.** The v5 API made verification safe by default: you must supply the expected
signing method, and `jwt.WithValidMethods`, `jwt.WithIssuer` and `jwt.WithExpirationRequired` are
explicit parser options. This closes the classic `alg: none` and RS256→HS256 confusion attacks that
older JWT libraries allowed through permissive defaults. Anything on `dgrijalva/jwt-go` (abandoned)
or v3/v4 should be migrated.

### `golang.org/x/crypto`

<https://pkg.go.dev/golang.org/x/crypto> · BSD-3-Clause

Used for `bcrypt` (password hashing) and `hkdf` (deriving the token-signing key from `JWT_SECRET`
with a distinct info label, so the two key usages never share material).

**Why bcrypt.** It is deliberately slow, salts automatically, and stores its cost factor inside the
hash string — so raising `BCRYPT_COST` later transparently re-hashes users on their next successful
login without a migration. Argon2id (also in `x/crypto`) is the more modern choice and is a
reasonable swap; bcrypt is the default because its memory profile is predictable on small
containers, where Argon2's memory parameter is easy to misconfigure into an availability problem.

HMAC and SHA-256 for verification tokens come from the standard library (`crypto/hmac`,
`crypto/sha256`), and all random values from `crypto/rand`. Never `math/rand`.

### `golang.org/x/oauth2`

<https://pkg.go.dev/golang.org/x/oauth2> · BSD-3-Clause

The OAuth 2.0 client, with a `google` sub-package holding Google's endpoints. It handles the
authorization-code exchange, token refresh and the `state` round-trip.

**Why include it in a template.** "Sign in with Google" is close to universally requested, and the
part people get wrong — validating `state` to prevent login CSRF — is exactly the part this library
structures for you. It is entirely optional: leave `GOOGLE_CLIENT_ID` empty and the route is not
registered.

---

## Identity

### `github.com/google/uuid`

<https://github.com/google/uuid> · BSD-3-Clause

RFC 4122 UUID generation and parsing. Every primary key in the template is a `uuid.UUID`.

**Why UUIDs over auto-increment integers.** IDs can be generated by the application before the row
exists, which makes multi-table writes inside one transaction straightforward; they do not leak row
counts or let a client enumerate `/users/1`, `/users/2`; and merging data between environments does
not collide. The cost is index locality — random UUIDs scatter B-tree inserts. If that shows up in
profiling, switch to UUIDv7 (`uuid.NewV7()`, time-ordered) which keeps the type and the URL shape
identical.

---

## Configuration

### `github.com/joho/godotenv`

<https://github.com/joho/godotenv> · MIT

Loads `.env` files into the process environment. The template loads three tiers in order —
`.env` → `.env.$GO_ENV` → `.env.$GO_ENV.local` — so shared defaults, per-environment values and
machine-local overrides layer cleanly.

**Why not Viper?** Viper is powerful and brings YAML/TOML/JSON files, remote config and live reload.
This template only ever wants environment variables, because that is what containers, systemd and
every PaaS actually inject — and a single source of truth beats a precedence chain nobody can
recall. godotenv is a convenience for local development on top of that model, not a config system
of its own. Parsing and validation are plain Go in `internal/config`, which means a missing or
malformed value is a typed error at startup instead of a zero value discovered in production.

**One sharp edge worth knowing:** the tiered loader uses `godotenv.Overload`, which *overwrites*
already-set process environment variables. That is what makes the tiers work, and it is also why the
test harness runs from an empty temporary directory — see [`TESTING.md`](TESTING.md).

---

## Logging & observability

### `github.com/rs/zerolog`

<https://github.com/rs/zerolog> · MIT

Structured, leveled, zero-allocation JSON logging. `LOG_FORMAT=console` gives a coloured
human-readable writer in development; `json` gives one object per line for log aggregators.

**Why zerolog.** Structured logs are the difference between grepping strings and querying fields,
and zerolog's chained API (`log.Info().Str("user_id", id).Dur("took", d).Msg("...")`) builds the
record without intermediate allocations. A request-scoped logger carrying the request ID is put on
the Gin context by middleware, so every line from a request correlates.

**Why not `log/slog`?** `slog` is in the standard library since Go 1.21 and is the safe long-term
default — no dependency, and the `slog.Handler` interface means everything can route through it.
zerolog is used here for its console writer and its ergonomics under load, but the logger is behind
`internal/shared/logger`, so swapping to `slog` is a single-package change. If you have no strong
preference, `slog` is a perfectly good simplification.

### `github.com/getsentry/sentry-go` and `.../sentry-go/gin`

<https://github.com/getsentry/sentry-go> · MIT

Error tracking. The Gin middleware recovers panics, attaches request context (method, route,
headers minus credentials, request ID) and reports them; the template additionally captures any
`AppError` that maps to HTTP 5xx, so genuine server faults are reported while 4xx client mistakes
are not — the distinction that keeps the issue feed usable.

Entirely optional: leave `SENTRY_DSN` empty and the middleware is not installed. Swapping in another
error tracker touches one file.

---

## API documentation

### `github.com/swaggo/swag`, `github.com/swaggo/gin-swagger`, `github.com/swaggo/files`

<https://github.com/swaggo/swag> · MIT

`swag` parses `// @Summary` / `// @Param` / `// @Success` comments above handlers and generates an
OpenAPI 2.0 spec into `docs/`. `gin-swagger` plus `swaggo/files` serve the Swagger UI at
`/swagger/index.html`.

**Why annotation-driven docs.** The alternative — a hand-maintained `openapi.yaml` — is accurate on
the day it is written and wrong a month later. Keeping the contract next to the handler means a
reviewer sees the doc change in the same diff as the code change.

**Its two real constraints**, both of which shape the layout: DTO types must live in the `service/`
package to resolve as `service.XxxDTO` in the generated spec, and the blank import
`_ "github.com/DevanshSharmaT-T/Go-Gin-Template/docs"` must be present in `main.go` or the binary
compiles and serves an empty spec. Regeneration is a manual `make swagger` — it is not wired into
live reload, so the committed spec can lag behind the code.

---

## Testing

### `go.uber.org/mock`

<https://github.com/uber-go/mock> · Apache-2.0

The maintained fork of `golang/mock`, which was archived. `mockgen` generates a mock implementation
for each repository interface into `internal/mocks/`; `make mocks` regenerates them.

**Why generated mocks over hand-written fakes.** The compiler enforces that the mock matches the
interface, so adding a repository method breaks the mock immediately rather than leaving tests
silently exercising a stale seam. `EXPECT()` also asserts *interaction* — that the service called
`Update` exactly once with the entity it claimed to — which hand-written fakes usually do not.

### `go.uber.org/fx/fxtest`

Ships with fx. Boots a real fx application inside a test and fails the test (rather than panicking
the process) if the graph does not build, running the same `OnStart`/`OnStop` hooks. It is the
foundation of `test/harness`, which is why integration tests exercise the production wiring instead
of a re-declared copy of it.

The standard library's `testing` package covers everything else; there is no assertion library, and
`net/http/httptest` drives the HTTP layer.

### `golang.org/x/time`

<https://pkg.go.dev/golang.org/x/time/rate> · BSD-3-Clause

`rate.Limiter`, the standard token-bucket implementation, backs the per-IP rate-limit middleware.
It is listed here rather than under HTTP because it is the one non-obvious dependency: a correct
token bucket with burst handling is easy to get subtly wrong by hand, and this is the canonical one.

Note that it is **in-process** — each replica enforces its own bucket. That is fine for slowing
credential stuffing on a single instance; a multi-replica deployment wants a shared store (Redis)
instead. Same caveat as the permission registry in [`ARCHITECTURE.md`](ARCHITECTURE.md).

---

## Tooling (not libraries)

These are binaries used during development. None of them is imported by the application, and none
is required to build or run it.

| Tool | Purpose | Install |
|---|---|---|
| [`air`](https://github.com/air-verse/air) | Live reload — rebuilds and restarts on save. Configured in `.air.toml`. | `go install github.com/air-verse/air@latest` |
| [`golangci-lint`](https://golangci-lint.run) | Aggregate linter. The config **is committed** as `.golangci.yml` so every machine and CI enforce the same rules. | `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest` |
| [`govulncheck`](https://go.dev/blog/govulncheck) | Reports known vulnerabilities *reachable from your code*, not merely present in `go.mod`. Also flags toolchain CVEs. | `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` |
| [`mockgen`](https://github.com/uber-go/mock) | Generates the mocks in `internal/mocks`. Run via `go run` so the version is pinned by `go.mod`. | `make mocks` |
| [`swag`](https://github.com/swaggo/swag) | Generates the OpenAPI spec from handler annotations. | `make swagger` |

---

## Where each library is used

| Library | Package it lives behind | Configuration keys |
|---|---|---|
| `gin-gonic/gin` | `cmd/api`, every `modules/*/api`, `shared/middleware` | `SERVICE_HOST`, `SERVICE_PORT`, `TRUSTED_PROXIES`, `MAX_REQUEST_BODY_BYTES` |
| `go-playground/validator/v10` | `modules/*/api` (binding), `shared/errors` (translation) | — |
| `gorm.io/gorm`, `driver/postgres` | `shared/database`, every `modules/*/infra` | `DATABASE_LOG_LEVEL` |
| `jackc/pgx/v5` | `shared/database` (driver, error codes) | `DATABASE_URL`, `DATABASE_MAX_OPEN_CONNS`, `DATABASE_MAX_IDLE_CONNS`, `DATABASE_CONN_MAX_LIFETIME`, `DATABASE_CONN_MAX_IDLE_TIME` |
| `go.uber.org/fx`, `dig` | `cmd/api`, every `module.go` | — |
| `golang-jwt/jwt/v5` | `modules/auth`, `shared/middleware` | `JWT_SECRET`, `JWT_ISSUER`, `JWT_TTL` |
| `golang.org/x/crypto` | `shared/crypt` | `BCRYPT_COST`, `TOKEN_SECRET`, `VERIFICATION_TOKEN_TTL`, `PASSWORD_RESET_TOKEN_TTL` |
| `golang.org/x/oauth2` | `modules/auth` | `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URL` |
| `google/uuid` | every `modules/*/domain` | — |
| `joho/godotenv` | `internal/config` | `GO_ENV` |
| `rs/zerolog` | `shared/logger`, `shared/middleware` | `LOG_LEVEL`, `LOG_FORMAT` |
| `getsentry/sentry-go` (+ `/gin`) | `shared/logger`, `shared/middleware` | `SENTRY_DSN`, `SENTRY_ENVIRONMENT`, `SENTRY_TRACES_SAMPLE_RATE` |
| `swaggo/swag`, `gin-swagger`, `files` | `cmd/api`, `docs/` | — |
| `golang.org/x/time` | `shared/middleware` | `RATE_LIMIT_ENABLED`, `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST`, `AUTH_RATE_LIMIT_RPM`, `AUTH_RATE_LIMIT_BURST` |
| `go.uber.org/mock`, `fx/fxtest` | `internal/mocks`, `test/harness` | `TEST_DATABASE_URL` |

Mail uses the standard library's `net/smtp` and `html/template`, with templates embedded via
`//go:embed` so the binary has no runtime file dependency. CORS, request IDs, timeouts and recovery
are hand-written middleware in `shared/middleware` — each is a few dozen lines, and owning them
means the security-relevant behaviour (an allow-list that never echoes an untrusted origin) is
visible in this repository rather than in a dependency.

---

## Deliberately absent

| Not used | Why |
|---|---|
| An assertion library (`testify`, …) | The standard `testing` package plus explicit `if got != want` keeps failure messages honest and adds no dependency. Add one if your team prefers it — nothing here depends on its absence. |
| A DTO mapping library | Hand-written `toDTO` functions are a few lines each and make it *visible* that `PasswordHash` is not copied into a response. Reflection-based mappers hide exactly that. |
| Redis / a cache layer | Nothing in the template needs one yet. When you add multi-replica rate limiting or session revocation, this is where it goes. |
| A migration tool (`goose`, `golang-migrate`) | Migrations are versioned Go functions driven by GORM (see [`ARCHITECTURE.md`](ARCHITECTURE.md#migrations)), so schema changes stay in Go alongside the models and run automatically at boot. Bring in `golang-migrate` if you want hand-written, reversible SQL and a CLI. |
| A message queue / background worker | Out of scope for a starter. Mail is sent inline; if that becomes a latency problem, that is the moment to introduce a queue rather than before. |

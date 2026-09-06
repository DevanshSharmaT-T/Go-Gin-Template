# Changelog

Notable changes to this template. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project intends to follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) once it is tagged.

There are no releases yet, so everything lives under `Unreleased`.

## [Unreleased]

The template is being built in phases. Each phase is a single commit; the `Planned` table below
lists what is still to come.

### Added

**Phase 1 — structure and documentation**

- Project skeleton: `cmd/api`, `internal/{config,shared,modules,mocks}`, `test/`, `docs/`, and the
  four-layer `api`/`service`/`domain`/`infra` directory shape for the `users`, `auth`, `roles`,
  `messages` and `health` modules.
- `README.md` — what the template is, its feature set, a clone-and-run quick start, the command
  reference, the layout, a "making it yours" guide and a troubleshooting section.
- `docs/LIBRARIES.md` — every dependency: what it does, why it was chosen, what the alternative
  would cost, and a table mapping each library to the packages and environment variables that use
  it.
- `docs/ARCHITECTURE.md` — the modular monolith and its hexagonal layering, the four fx wiring rules
  that fail at startup rather than at compile time, the boot sequence, the `AppError` model, the
  RBAC model including its single-instance scaling limit, and the reasoning behind the
  `var`-over-`:=` convention.
- `docs/TESTING.md` — the unit / integration / e2e layers, the harness that boots the real
  application, build tags, and the `godotenv.Overload` trap the harness works around.
- `CONTRIBUTING.md` — branch and commit conventions, the enforced code style, how to add a module
  and a migration, and the pre-PR checklist.
- `SECURITY.md` — private vulnerability reporting through GitHub advisories, the security decisions
  baked into the template, and a pre-deployment hardening checklist.
- `LICENSE` — Apache-2.0.
- `.env.example` — every configuration key with its default and whether it is required, placeholders
  only.
- `Makefile` with a `help` target, `.air.toml` for live reload, a committed `.golangci.yml`, and
  `.gitignore`.
- GitHub Actions CI (build · vet · test · integration · e2e · govulncheck · lint) and a pull-request
  template.

**Phase 2 — Go module and foundations**

- `go.mod` declaring module `github.com/DevanshSharmaT-T/Go-Gin-Template` on Go 1.25, with three
  direct dependencies: `joho/godotenv`, `rs/zerolog` and `getsentry/sentry-go`.
- `internal/shared/errors` — the `AppError` kernel: 22 classifications, one type-to-HTTP-status
  mapping, and 22 constructors. `Response()` redacts the message and drops details for 5xx so
  operator-facing text and driver errors cannot reach a client, while the cause stays reachable for
  the log. Re-exports `As`/`Is`/`Join`/`Unwrap` so callers do not need the standard `errors` too.
- `internal/config` — tiered `.env` loading (`.env` → `.env.$GO_ENV` → `.env.$GO_ENV.local`) with
  typed parsing and startup validation. Problems are accumulated and reported together rather than
  one per restart. An explicitly exported `GO_ENV` is preserved across the tier load, so
  `GO_ENV=production ./server` cannot be downgraded by a stale line in `.env`.
- `internal/shared/logger` — zerolog setup (JSON or console), a request-scoped logger carried on
  `context.Context`, and optional Sentry initialisation that is a no-op without a DSN.
- 54 package-local tests covering the status mapping, 5xx redaction, the wire format, every
  validation rule, and the logger's context fallback.
- `test/integration` and `test/e2e` package declarations, so the tagged CI suites resolve instead
  of failing on "no packages to test".

**Phase 3 — the database layer**

- `internal/shared/database` — the GORM/pgx connector, with all four `DATABASE_*` pool settings
  actually applied to the underlying `sql.DB`, `TranslateError` enabled so constraint violations
  arrive as GORM sentinels, and UTC timestamps rather than GORM's server-local default. Automatic
  pinging is off: a constructor has no context, so the real check happens in the start hook, where
  it can honour a deadline.
- `internal/shared/database/errors.go` — `TranslateError`, the seam between the driver and the rest
  of the application. It maps GORM sentinels and PostgreSQL SQLSTATE codes onto `AppError`
  classifications (`23505` → `CONFLICT`, `23502`/`23514` → `VALIDATION`, `57014` → `TIMEOUT`,
  class `08` → `UNAVAILABLE`, everything else → `INTERNAL`) using only `errors.Is`/`errors.As` —
  no error text is matched, and an already-classified `AppError` is passed through unchanged.
- `internal/shared/database/migrations` — the versioned migration runner. It validates that versions
  are contiguous from 1 *before* applying anything, applies each pending version and its
  `migration_records` row in one transaction, and checks every error rather than discarding the ones
  from `Pluck` and the commit. The catalogue ships empty, with a commented recipe for each change
  `AutoMigrate` cannot express.
- `internal/shared/database/module.go` — the fx module. It consumes the `group:"models"` and
  `group:"seeders"` value groups and hangs the boot sequence off `fx.Lifecycle`: ping, create the
  `uuid-ossp` extension, `AutoMigrate` the models registry, apply versioned migrations, seed, and
  close the pool on shutdown. Every step takes the start context, so `fx.StartTimeout` can cancel a
  migration that is blocked on a lock. **An entry point must raise that timeout above fx's 15-second
  default** if seeding a fresh database takes longer.
- `internal/shared/database/seed.go` — `SeedFunc` and a runner that names the failing seeder in its
  error, since a value group is unordered and its members are otherwise anonymous.
- `internal/shared/database/logger.go` — a GORM `logger.Interface` bridged onto zerolog, honouring
  `DATABASE_LOG_LEVEL`, warning about statements over 200ms and never treating
  `gorm.ErrRecordNotFound` as a failure.
- `internal/config/module.go` — `fx.Provide(Load)`, so configuration resolves through the graph.
- `test/harness` — `harness.App` boots the real fx graph against `TEST_DATABASE_URL` and populates
  the targets a test asks for, skipping when that variable is unset. It runs from an empty temporary
  directory so `godotenv.Overload` cannot swap the test DSN for the developer's own.
- 64 further package-local tests, none of which need a database: every `TranslateError` branch, the
  contiguity guard, the GORM log-level mapping and the seeder runner.
- A 10-test integration suite that boots the real graph with a test-local model and seeder
  registered through the same value-group tags a feature module uses, asserting that the table is
  created, `migration_records` is populated, a failed migration is rolled back and left unrecorded,
  a second boot changes nothing, and a duplicate insert surfaces as a `CONFLICT` with the constraint
  name absent from the response. No throwaway table enters the template itself.

There is still no `cmd/api/main.go`, so `make build` and `make run` do not work yet. The database
layer is library code that phase 4 wires up.

**Phase 4 — users, authentication and the entry point**

- `cmd/api` — the entry point. `make build` and `make run` work for the first time: the Gin engine
  (with Gin's own stdout logger and recovery left off, since the structured equivalents arrive with
  the middleware phase), the three-tier route groups, and an `http.Server` with every timeout set
  and a graceful drain on shutdown. The listener is opened *inside* the start hook, so "port already
  in use" fails the boot instead of being logged from a goroutine after fx reports success.
- `internal/app` — the single list of modules. `cmd/api` composes it and so does `harness.App`, so
  the harness demonstrably boots the same graph that is deployed rather than a subset of it.
- `internal/shared/crypt` — bcrypt hashing at the configured cost (not `bcrypt.DefaultCost`),
  constant-time comparison, `NeedsRehash` so raising `BCRYPT_COST` re-hashes users on their next
  login with no migration, `DummyCompare` for the unknown-user path, `crypto/rand` helpers, and the
  HMAC token codec. The token key is derived from `JWT_SECRET` with HKDF under a distinct info
  label when `TOKEN_SECRET` is unset, so the two key usages never share material.
- `internal/shared/middleware` — the `Claims` type, the four documented context keys, the HS256
  token codec and `JWTMiddleware`. Verification pins the algorithm, requires the issuer and requires
  an expiry claim, all explicitly.
- `internal/shared/mail` — the `Mailer` port and the log driver, so the verification and reset flows
  work end to end on a clone with no SMTP server. The SMTP driver arrives with the mail phase;
  asking for `MAIL_DRIVER=smtp` today logs a warning rather than silently not delivering.
- `internal/modules/users` — the `User` and `VerificationToken` entities, both repository ports and
  their GORM adapters, the validation rules, the account service and its handler
  (`/api/users/me`, list, get, status).
- `internal/modules/auth` — registration, login, email verification, and password reset, with the
  `RoleResolver` port the RBAC phase fills in.
- 90 package-local and unit tests, and 7 further integration tests covering the register → verify →
  login flow, single-use token redemption, reset-link supersession and purpose confinement against
  a real database.

**Not yet, and it matters:** roles and permissions are the next phase, so `middleware.Authorize`
does not exist and the administrative user routes are authenticated but ungated; the `TokenGuard`
and `RoleResolver` seams ship with placeholders that accept every valid token and grant no
permissions. Tokens therefore stay valid for their full TTL after a suspension. Do not deploy
before that phase.

**Phase 5 — roles, permissions and authorization**

- `internal/modules/roles` — the `Role`, `Permission` and `RolePermission` entities, the repository
  port and its GORM adapter, the role service, and the handler behind `/api/roles`.
- `roles/domain/catalogue.go` — **one declaration per permission**, named by both the route and the
  seeder. The failure this prevents is a route gated on `case:view` while the seeder inserts
  `cases:view`: it fails closed, silently removing access, and it survives review because neither
  file looks wrong alone. With one constant a typo is a compile error, and a constant missing from
  the catalogue is caught by a test.
- `roles/domain/registry.go` — the in-memory permission registry: each role's grants, its permission
  version, its hierarchy level, and the set of suspended accounts. It is a package-level singleton
  that fx does not manage, rebuilt at boot, and every method is safe for concurrent use.
- `internal/shared/middleware/authorize.go` — `Authorize(slug, level)` and `RequireLevel(level)`.
  Both conditions must hold; level 1 bypasses the permission map as the documented break-glass; a
  route with no authentication in front of it is refused rather than allowed.
- Both revocation gates now work, and neither touches the database. Changing a role's permissions
  bumps its version in the same transaction as the grant change, so every token that role has
  outstanding is refused with `TOKEN_STALE` from the next request. Suspending an account adds it to
  the registry in the same call that writes the row, so `USER_SUSPENDED` is immediate rather than
  waiting out the token's TTL. Suspensions are read back at boot, so a restart does not lift them.
- Seeders for the role hierarchy, the permission catalogue and the initial grants, all idempotent
  with `ON CONFLICT DO NOTHING` — so a permission an administrator has revoked through the API is
  not restored on the next restart.
- `users/seeds/admin_seeder.go` — the optional first administrator from `ADMIN_*`. It logs a warning
  when it skips, a louder one when the three variables are half-set, and it carries no personal data
  of any kind.
- The administrative routes are gated: `users:list`/`users:read` at manager level, `users:manage`
  and `roles:manage` at administrator level.
- 37 further package-local tests — including the hierarchy direction, the two-condition rule, the
  break-glass, and a race-detected concurrency test on the registry — and 8 further integration
  tests covering the warm-up, revocation on permission change, immediate suspension, and suspension
  surviving a restart.

### Removed

- The two placeholders the auth phase shipped. `FallbackRoleResolver` is deleted, and `AllowAllGuard`
  is now documented as a test double that the application does not provide. Neither is wired, so a
  graph that still referenced one fails to build rather than silently downgrading to it.

### Changed

- `.env.example` — `GO_ENV` now documents `test` as a fourth valid tier and notes that it is
  validated at startup.

### Fixed

- **`make lint` could never pass, on any commit.** Two staticcheck checks — `ST1023` and `QF1011`,
  both "omit the redundant type from this declaration" — are the exact inverse of this project's
  `var`-with-explicit-type rule, so every conforming declaration was a lint error. `gosec`'s `G101`
  separately read the error-type vocabulary (`TypeInvalidCredentials`, `TypeTokenExpired`) as
  hardcoded credentials. Both are now scoped exclusions in `.golangci.yml` with the reasoning next
  to them, and `golangci-lint run` reports zero issues.
- `.golangci.yml` now sets `build-tags: [integration, e2e]`. golangci-lint honours build
  constraints, so the two suites that touch a real database were not being analysed at all.
- `internal/config` — an unchecked `os.Setenv` return, a `WriteString(fmt.Sprintf(...))`, and a test
  helper that type-asserted on an error instead of using `errors.As` (it would have missed a wrapped
  `*ValidationError`).
- **`make test-db-up` could return before PostgreSQL was actually accepting connections.** The
  postgres image runs a temporary server during initialisation with `listen_addresses=''`, so
  `pg_isready` over the Unix socket succeeds roughly a second before that server is shut down and
  the real one starts. The `sleep 2` that followed usually covered the gap; when it did not, the
  suite began against a database that then restarted underneath it, and the tests running at that
  moment failed instantly for no visible reason. Both the Makefile and the CI service health check
  now ask over TCP (`pg_isready -h 127.0.0.1`), which the temporary server does not answer, and the
  Makefile wait is bounded with an actionable message instead of looping forever.

### Security

- Configuration that would be unsafe is now a startup failure rather than a runtime surprise:
  a `JWT_SECRET` that is empty, under 32 characters or still the `CHANGE_ME` placeholder;
  `CORS_ALLOWED_ORIGINS=*` combined with `CORS_ALLOW_CREDENTIALS=true`; a `REQUEST_TIMEOUT` that
  is not shorter than `SERVER_WRITE_TIMEOUT`; a non-positive body-size cap; an unparseable entry
  in `TRUSTED_PROXIES`; and a half-configured Google OAuth credential pair.
- Secret values are never echoed into a configuration error message.
- Bound query parameters are never rendered into a log line. The GORM bridge implements
  `gorm.ParamsFilter` and drops them unconditionally, so a logged statement carries `$1` and not the
  email address, token or password substituted into it.
- **A permission slug is declared once**, and named by both the route and the seeder. The classic
  version of this bug — a route gated on a slug that was never seeded — fails closed and silently
  removes access from everyone who should have had it.
- **Authorization requires a permission *and* a seniority level**, and a refusal does not say which
  one failed. Reporting "you have the permission but not the seniority" describes the shape of the
  model and which accounts are worth attacking.
- **`Authorize` refuses when it cannot find claims**, so a route registered without authentication
  in front of it is closed rather than open.
- **Registration cannot assign a role.** `RegisterRequestDTO` has no `role_id` field at all, so a
  request carrying one is ignored by the decoder rather than filtered later; the role comes from a
  server-side constant. Covered by a unit test asserting what reaches the repository.
- **Login rejects non-active accounts before checking the password**, and every failure — unknown
  identifier, wrong password, inactive, suspended — returns byte-identical output. An unknown
  identifier still runs a bcrypt comparison against a fixed hash, so "no such user" is not
  measurably faster than "wrong password".
- **Password-reset and verification links carry an opaque, single-use, short-lived token**, never a
  password hash. Single use is enforced by a conditional `UPDATE ... WHERE consumed_at IS NULL`, so
  two requests racing on the same link cannot both win, and only a SHA-256 fingerprint of the token
  is stored — a leaked table yields no working links. Issuing a new link retires the previous one.
- **Verification tokens are JSON-serialised and the HMAC covers those exact bytes**, verified
  *before* the payload is unmarshalled. The purpose is signed in and checked, so a verification
  token cannot be redeemed as a password reset.
- **The forgot-password endpoint answers identically** whether or not the account exists.
- Driver detail stays server-side. `TranslateError` keeps the `*pgconn.PgError` — with its
  constraint name, table name and offending value — as the `AppError`'s cause, which is logged and
  never serialised; the caller gets the classification and a generic message.

### Planned

| Phase | Contents |
|---|---|
| 6 | Email and messages — SMTP mailer with embedded templates, the notifications module |
| 7 | Middleware and health — CORS allow-list, request ID, rate limiting, timeouts, probes |
| 8 | Tests and tooling — harness, fixtures, mocks, the three suites, Docker, Swagger |

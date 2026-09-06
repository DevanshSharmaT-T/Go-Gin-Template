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

### Changed

- `.env.example` — `GO_ENV` now documents `test` as a fourth valid tier and notes that it is
  validated at startup.

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
- Driver detail stays server-side. `TranslateError` keeps the `*pgconn.PgError` — with its
  constraint name, table name and offending value — as the `AppError`'s cause, which is logged and
  never serialised; the caller gets the classification and a generic message.

### Planned

| Phase | Contents |
|---|---|
| 4 | Users and auth — the user module, JWT, bcrypt, the HMAC token codec |
| 5 | RBAC — roles, permissions, the `Authorize` middleware |
| 6 | Email and messages — SMTP mailer with embedded templates, the notifications module |
| 7 | Middleware and health — CORS allow-list, request ID, rate limiting, timeouts, probes |
| 8 | Tests and tooling — harness, fixtures, mocks, the three suites, Docker, Swagger |

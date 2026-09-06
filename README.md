# Go + Gin Backend Template

A production-shaped starting point for a Go REST API. Clone it, point it at a PostgreSQL database,
and you have a running service with authentication, role-based access control, migrations,
structured logging, OpenAPI docs and a three-layer test suite already wired together.

**Gin** (HTTP) · **GORM + PostgreSQL** (persistence) · **Uber fx** (dependency injection) ·
**HS256 JWT** (auth) · **zerolog** (logging) · **swaggo** (OpenAPI)

Licensed under [Apache-2.0](LICENSE) — use it for anything, including commercially.

---

## Contents

- [What this is](#what-this-is)
- [Features](#features)
- [Requirements](#requirements)
- [Quick start](#quick-start)
- [Making it yours](#making-it-yours)
- [Commands](#commands)
- [Project layout](#project-layout)
- [API documentation](#api-documentation)
- [Documentation map](#documentation-map)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [License](#license)

---

## What this is

Most Go starters are either a single `main.go` that teaches you nothing about structure, or a
framework that hides the structure entirely. This one sits in between: it is a **modular monolith**
with the boundaries drawn explicitly, so the tenth feature costs about what the first one did.

Every decision it makes is written down. [`docs/LIBRARIES.md`](docs/LIBRARIES.md) explains why each
dependency is present and what the alternative would have cost;
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) explains the layering, the wiring rules and the
conventions. You are meant to disagree with some of it and change those parts — that is easier when
the reasoning is on the page rather than in someone's head.

> **Status.** The template is being built in phases. In place today: the skeleton, tooling and
> documentation (phase 1); configuration, structured logging and the `AppError` kernel (phase 2);
> the database layer, migration and seeder runners and the test harness (phase 3); users and auth
> with the entry point (phase 4); and RBAC — roles, permissions, `Authorize`, and both instant
> revocation gates (phase 5).
>
> Mail is delivered over SMTP with embedded HTML and text templates (phase 6a).
>
> **Still to come:** the notifications module, then CORS, request IDs, rate limiting and request
> timeouts, then the health probes, Docker and Swagger. **The transport hardening in that list is
> the reason not to expose this publicly yet** — the authorization model itself is complete.
> See [`CHANGELOG.md`](CHANGELOG.md) for exactly what exists and what is next.

---

## Features

**HTTP & structure**
- Modular monolith with hexagonal layering per module — `api` → `service` → `domain` ← `infra`
- Three-tier routing: public, authenticated, permission-gated
- Uber fx dependency injection with ordered startup/shutdown hooks
- Graceful shutdown that drains in-flight requests

**Auth & access control**
- Email/password registration and login, bcrypt hashing with a configurable cost
- HS256 JWTs carrying a flattened permission map, so authorization never hits the database
- Role hierarchy plus `resource:action` permission slugs
- Instant revocation without a database round-trip: permission versioning and user suspension
- Email verification and password reset with HMAC-signed, short-lived, single-use tokens
- Optional Google OAuth sign-in

**Data**
- GORM + PostgreSQL over the pgx driver, with a tuned connection pool
- Versioned migrations that run automatically at boot and are recorded in the database
- Idempotent seeders for roles, permissions and an optional first administrator
- `uuid.UUID` primary keys throughout

**Operations**
- Structured zerolog logging with per-request IDs; console output in development, JSON elsewhere
- Optional Sentry error tracking that reports 5xx and ignores client mistakes
- Liveness and readiness endpoints
- Configuration validated at startup — the process refuses to boot on an unsafe setting

**Hardened by default**
- CORS allow-list (`*` with credentials is rejected), per-IP rate limiting with a tighter bucket on
  auth routes, request timeouts, body-size caps, no `X-Forwarded-For` trust unless configured
- A single `AppError` type: no driver text or stack traces ever reach a client

**Developer experience**
- Live reload via `air`, a `make help` menu, a committed `.golangci.yml`
- Swagger UI generated from handler annotations
- Unit / integration / e2e test layers with a harness that boots the *real* application
- GitHub Actions CI: build · vet · test · govulncheck · lint

---

## Requirements

| | Version | Notes |
|---|---|---|
| [Go](https://go.dev/dl/) | **1.25+** | the only hard requirement |
| PostgreSQL | 14+ | the role must be able to `CREATE EXTENSION "uuid-ossp"` |
| Docker | any | optional — the fastest way to get PostgreSQL |
| [air](https://github.com/air-verse/air) | latest | optional — live reload for `make dev` |
| [swag](https://github.com/swaggo/swag) | latest | optional — only to regenerate the OpenAPI spec |

`make tools` installs the optional three.

---

## Quick start

```bash
git clone https://github.com/DevanshSharmaT-T/Go-Gin-Template.git
cd Go-Gin-Template

# 1. Configuration. No .env is committed, so this step is not optional.
cp .env.example .env

# 2. A database. Anything reachable works; this is the quickest:
docker run -d --name gin-template-db \
  -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=app \
  -p 5432:5432 postgres:16

# 3. Generate a real JWT secret and put it in .env (the placeholder is rejected).
openssl rand -base64 48

# 4. Run. Migrations and seeders execute automatically on boot.
make run          # or: make dev   (live reload)
```

The API listens on `SERVICE_HOST:SERVICE_PORT` — `127.0.0.1:8080` by default.

```bash
curl http://localhost:8080/api/health
open  http://localhost:8080/swagger/index.html
```

Only two variables must be set before the first boot: **`DATABASE_URL`** and **`JWT_SECRET`**.
Everything else in [`.env.example`](.env.example) has a working default. Set `ADMIN_USERNAME`,
`ADMIN_EMAIL` and `ADMIN_PASSWORD` as well if you want an administrator account created on the first
run.

There is no separate migration step. On every boot the app connects, creates the `uuid-ossp`
extension, applies pending migrations, runs the idempotent seeders, warms the permission registry
and starts serving.

---

## Making it yours

The template is deliberately generic, so adopting it is mostly renaming:

1. **Change the module path.** Replace `github.com/DevanshSharmaT-T/Go-Gin-Template` in `go.mod`
   with your own, then update the imports:

   ```bash
   go mod edit -module github.com/you/your-api
   grep -rl 'github.com/DevanshSharmaT-T/Go-Gin-Template' --include='*.go' . \
     | xargs sed -i 's|github.com/DevanshSharmaT-T/Go-Gin-Template|github.com/you/your-api|g'
   go mod tidy
   ```

   Do the same for `local-prefixes` in `.golangci.yml`.

2. **Set `APP_NAME`** in `.env` — it feeds the logger, the JWT issuer and mail subjects.

3. **Delete what you do not need.** The `messages` module, Google OAuth and Sentry are all optional
   and self-contained. Removing a module means deleting its directory and its `fx.Provide` lines.

4. **Replace this README**, and adjust `SECURITY.md` to point at your own reporting channel.

5. **Decide about the `var` convention.** The template requires `var x T = ...` over `:=` and
   [explains why](docs/ARCHITECTURE.md#declarations-use-var-with-an-explicit-type). It is a style
   choice; nothing depends on it. Drop it if your team prefers idiomatic Go.

---

## Commands

```bash
make help              # every target, with descriptions

make dev               # live reload (air), GO_ENV=development
make run               # run once, no live reload
make build             # static linux/amd64 binary -> bin/server
make clean             # remove bin/, tmp/ and coverage output

make test              # unit + package tests, race detector, no database
make test-cover        # the same, with a coverage profile
make test-db-up        # disposable PostgreSQL on :55433
make test-integration  # real fx graph against a real database
make test-e2e          # the compiled server, driven over HTTP
make test-all
make test-db-down
make mocks             # regenerate repository mocks

make vet               # go vet — the shared baseline
make lint              # golangci-lint, using the committed .golangci.yml
make vuln              # govulncheck
make check             # vet + test, i.e. what CI runs

make swagger           # regenerate the OpenAPI spec into docs/
make tools             # install air, golangci-lint and swag
```

Run one test:

```bash
go test ./internal/shared/crypt/... -run TestVerificationToken_RoundTrip -v
```

---

## Project layout

```
cmd/api/                  entry point: the Gin engine, route registration, the HTTP server
internal/
  app/                    the one list of modules, composed by cmd/api and the test harness
  config/                 tiered env loading, parsing and startup validation
  shared/                 the shared kernel — used by every module
    errors/                 AppError and the single type -> HTTP status mapping
    database/               GORM/pgx connection, migrations, seeders
    crypt/                  bcrypt, HMAC tokens, secure random
    mail/                   SMTP and log drivers, embedded templates
    middleware/             JWT, Authorize, CORS, rate limit, request ID, timeout, recovery
    logger/                 zerolog setup and optional Sentry wiring
  modules/                one vertical slice per domain
    users/  auth/  roles/  messages/  health/
  mocks/                  generated repository mocks
test/
  harness/  fixtures/  unit/  integration/  e2e/  testdata/
docs/                     architecture, libraries, testing + the generated OpenAPI spec
.github/workflows/        CI
```

Every module has the same four layers:

```
api/       Gin handler — bind the DTO, delegate, render
service/   business rules and DTOs
domain/    GORM model + repository interface (the port)
infra/     GORM implementation of that interface (the adapter)
```

[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) explains why, and documents the dependency-injection
rules you have to follow when adding a module.

---

## API documentation

Swagger UI is served at `/swagger/index.html` while the app is running. Protected endpoints expect
`Authorization: Bearer <jwt>`; get a token from `POST /api/auth/login`.

The spec is generated from annotations above the handlers. Regeneration is manual — run
`make swagger` after changing an annotation, or the committed spec drifts.

---

## Documentation map

| Document | What it covers |
|---|---|
| [`docs/LIBRARIES.md`](docs/LIBRARIES.md) | every dependency: what it does, why it was chosen, what the alternative costs |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | layering, fx wiring rules, boot sequence, error model, RBAC, code style |
| [`docs/TESTING.md`](docs/TESTING.md) | the three test layers, the harness, build tags, conventions |
| [`.env.example`](.env.example) | every configuration key, its default, and whether it is required |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | branch and commit conventions, code style, how to add a module, PR checklist |
| [`SECURITY.md`](SECURITY.md) | reporting a vulnerability, and the pre-deployment hardening checklist |
| [`CHANGELOG.md`](CHANGELOG.md) | notable changes |
| [`LICENSE`](LICENSE) | Apache-2.0 |

---

## Troubleshooting

**The process exits immediately with a configuration error.**
Configuration is validated at startup and the message names the offending key. The usual causes are
an unset `DATABASE_URL`, or a `JWT_SECRET` that is empty, shorter than 32 characters, or still the
placeholder from `.env.example`. This is deliberate — booting with a broken security setting is
worse than not booting.

**`failed to connect to database`.**
`DATABASE_URL` is wrong or PostgreSQL is not reachable. Migrations, seeders and the permission
warm-up all run during boot, so the app cannot start without it.

**`CREATE EXTENSION "uuid-ossp"` permission denied.**
The database role needs superuser, or an administrator must install the extension once:
`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`

**Login works but every protected route returns 403.**
The permission registry is warmed at boot from the roles and permissions tables. If seeding was
skipped, or the database was swapped under a running process, restart the app. If it persists,
check that the slug on the route matches the slug in the seeder exactly — a mismatch fails closed.

**`401` with `"type": "TOKEN_STALE"`.**
Expected after any permission change. Tokens are versioned and outstanding ones are invalidated on
purpose. Log in again.

**A CORS error in the browser.**
`CORS_ALLOWED_ORIGINS` is an exact-match allow-list with no wildcard. Add the origin, scheme and
port included: `http://localhost:5173` does not match `http://localhost:3000`.

**A new endpoint returns 404 even though the handler exists.**
fx builds lazily. A handler that is provided but not passed to the route-registration function is
never constructed. See
[the fx rules](docs/ARCHITECTURE.md#dependency-injection-rules-that-fail-at-startup-not-at-compile-time).

**A new table was never created.**
Its model is missing the `group:"models"` provider stanza. Same section as above.

**Integration or e2e tests are skipped.**
`TEST_DATABASE_URL` is unset. That is by design, so `go test ./...` passes on a machine without a
database. Run `make test-db-up` first.

---

## Contributing

Issues and pull requests are welcome. Read [`CONTRIBUTING.md`](CONTRIBUTING.md) first — it covers
the conventions and the checklist. Security problems go through
[private reporting](SECURITY.md), not a public issue.

---

## License

[Apache License 2.0](LICENSE). You may use, modify and distribute this template, including
commercially. It comes with no warranty — see the license for the full terms.

# Architecture

How this template is put together, and why. Read this before your first change — several rules here
are deliberate and non-obvious, and a change that ignores them tends to fail at startup rather than
at compile time.

- [At a glance](#at-a-glance)
- [Modular monolith](#modular-monolith)
- [Hexagonal layering inside a module](#hexagonal-layering-inside-a-module)
- [The shared kernel](#the-shared-kernel)
- [Dependency injection: rules that fail at startup, not at compile time](#dependency-injection-rules-that-fail-at-startup-not-at-compile-time)
- [Boot sequence](#boot-sequence)
- [Request lifecycle](#request-lifecycle)
- [The error model](#the-error-model)
- [Auth and RBAC](#auth-and-rbac)
- [Migrations](#migrations)
- [Configuration](#configuration)
- [Code style](#code-style)
- [Security decisions baked in](#security-decisions-baked-in)

---

## At a glance

| Concern | Choice |
|---|---|
| Shape | Modular monolith — one binary, hard module boundaries |
| Layering | Hexagonal (ports & adapters) inside every module |
| HTTP | Gin, three-tier route groups |
| Persistence | GORM + PostgreSQL via pgx, repository interfaces as the seam |
| Wiring | Uber fx — constructor graph plus lifecycle hooks |
| Auth | HS256 JWT carrying a flattened permission map |
| Errors | A single `*AppError` type; handlers never choose status codes |
| Config | Environment variables, tiered `.env` files, validated at startup |

Every library choice is justified in [`LIBRARIES.md`](LIBRARIES.md).

---

## Modular monolith

One deployable binary, but modules are separated the way they would be if they were services.

The reasoning is about cost of change. Microservices buy independent deployment and scaling at the
price of network calls, distributed transactions, versioned contracts and a deployment pipeline per
service — costs you pay from day one and benefits you collect much later, if ever. A modular
monolith gives you the *boundary* discipline immediately and defers the *distribution* cost until
something actually needs to scale separately. When that day comes, a module that already talks to
the rest of the system through interfaces is a candidate for extraction; a module tangled into
everything else is not.

```
internal/modules/
  users/      accounts, profile, registration
  auth/       login, tokens, password reset, OAuth
  roles/      roles, permissions, the RBAC registry
  messages/   notifications and outbound mail records
  health/     liveness and readiness probes
```

The boundary rule: **a module may import its own packages and `internal/shared`. It may not import
another module's `infra` or `api` packages.** Cross-module needs go through the other module's
`domain` interface or its `service`, both of which are stable contracts. The one place this is
relaxed is `auth`, which by nature reads the user and role repositories — it depends on those two
`domain` packages and nothing else.

---

## Hexagonal layering inside a module

Every module has the same four directories, and dependencies point inward:

```
internal/modules/<name>/
  api/       <entity>_handler.go      Gin handler: bind, delegate, render
  service/   <entity>_service.go      orchestration and business rules
             <entity>_dto.go          request/response shapes
  domain/    <entity>_model.go        GORM struct + TableName()
             <entity>_repository.go   the repository INTERFACE (the port)
  infra/     <entity>_repository.go   GORM implementation (the adapter)
  module.go                           fx providers for this module
```

**`domain/` is the core.** It defines the entity and declares what storage must be able to do. It
imports no framework — no Gin, no HTTP, no SQL beyond the GORM struct tags that describe the table.
That is what lets business rules be tested with nothing but a mock.

**`infra/` is the adapter.** It is the only place `gorm.io/gorm` and pgx appear. It translates the
interface into queries and, critically, translates *driver* errors into domain errors:
`gorm.ErrRecordNotFound` becomes a `NOT_FOUND` `AppError`, pgx error code `23505` becomes a
`CONFLICT`. Nothing above this layer ever sees a driver error.

**`service/` is the orchestration.** It holds the rules — "a user must be active to log in", "the
role assigned at registration is the default role, never one the client asked for" — and it composes
the shared kernel: hashing, mail, tokens. It returns DTOs, never entities, which is the mechanism
that keeps `PasswordHash` out of responses.

**`api/` is the transport port.** Bind the DTO, call the service, render. A handler that contains an
`if` about business state belongs in the service; a handler that contains an `if` about HTTP status
codes is a bug (see [the error model](#the-error-model)).

`health/` legitimately has no `domain` or `infra` — it reports on dependencies it does not own — and
`auth/` has no `infra`, because it borrows the user and role repositories. Their empty directories
are placeholders kept for symmetry; delete them if you prefer.

**Why the entity/DTO split is not ceremony.** They change for different reasons. The entity changes
when the database does; the DTO changes when the API contract does. Collapsing them means every
column you add is immediately public, and every field you must not expose depends on remembering a
`json:"-"` tag. Two types and an explicit `toDTO` function make the omission visible in review.

---

## The shared kernel

`internal/shared` holds what every module needs, so no module reinvents it:

| Package | Responsibility |
|---|---|
| `errors` | `AppError`, its types, and the single mapping from type to HTTP status |
| `database` | GORM/pgx connection, pool configuration, migration runner, seeders |
| `crypt` | bcrypt hashing, HMAC verification tokens, secure random |
| `mail` | SMTP and log drivers, templates embedded with `//go:embed` |
| `middleware` | JWT auth, `Authorize`, CORS, rate limit, request ID, timeout, recovery |
| `logger` | zerolog setup, request-scoped loggers, optional Sentry wiring |

Shared code must not import a module. If it wants to, the thing it wants belongs in a module's
`service`, or it needs an interface that the module implements.

---

## Dependency injection: rules that fail at startup, not at compile time

fx resolves the graph by reflecting over constructor signatures. That is what makes it convenient
and it is also its one real cost: **wiring mistakes are startup errors, not build errors.** Four
specific mistakes account for nearly all of them.

### 1. Interface binding comes from the constructor's declared return type

There is no `fx.As` in this repository. A repository resolves as its port *only* because the
constructor says so:

```go
// Correct — registers under domain.UserRepository
func NewGormUserRepository(db *gorm.DB) domain.UserRepository

// Wrong — registers under *GormUserRepository; every service that depends on
// domain.UserRepository now fails to resolve, with a "missing type" error
func NewGormUserRepository(db *gorm.DB) *GormUserRepository
```

### 2. `fx.Annotate` is for value groups only

Two groups exist, and both are how a module registers itself without editing a central file:

- **`group:"models"`** — each persisted entity contributes `&domain.X{}`, which feeds `AutoMigrate`.
  **Omit this stanza and the table is simply never created.** The app boots fine and fails on the
  first query.
- **`group:"seeders"`** — the provider must declare the explicit `database.SeedFunc` return type. A
  structurally identical `func(*gorm.DB) error` lands in a different group slot and never runs.

### 3. `fx.Invoke` is the only thing that forces construction

fx builds lazily: anything not reachable from an `fx.Invoke` parameter list is never constructed. A
handler that is provided but not injected into route registration does not exist at runtime — its
endpoints 404 despite the code being present and compiling. **Adding a handler means adding it to
the route-registration function's parameters**, not just providing it.

### 4. One bare `gin.HandlerFunc` per graph

fx keys providers by type. Two providers returning `gin.HandlerFunc` collide with a duplicate-type
error at startup. Give every middleware a named type:

```go
type RateLimitMiddleware gin.HandlerFunc
type RequestIDMiddleware gin.HandlerFunc
```

> **How to debug any of these.** fx prints the full graph on startup failure and names the missing
> or duplicated type. Run with `fx.WithLogger` at debug level and read the constructor list — the
> first `[Error]` line names the exact type it could not resolve.

---

## Boot sequence

The `OnStart` hook order is load-bearing:

```
1. Load and validate configuration        ← fails fast on a bad or placeholder JWT_SECRET
2. Initialise the logger (and Sentry)
3. Connect to PostgreSQL and ping it
4. CREATE EXTENSION IF NOT EXISTS "uuid-ossp"
5. Run versioned migrations               ← recorded in migration_records
6. Run seeders                            ← idempotent, every boot
7. Warm the permission registry           ← reads roles + permissions into memory
8. Start the HTTP server
```

Step 7 must follow step 6. The permission seeder populates the in-memory registry that authorization
reads; reverse them and the first login issues a token with an empty permission map, which fails
closed — every protected route returns 403 until the process restarts.

Shutdown reverses it: stop accepting connections, wait up to `SHUTDOWN_TIMEOUT` for in-flight
requests, flush the logger and Sentry, close the pool.

---

## Request lifecycle

```
request
  → recovery ─ request ID ─ logger ─ CORS ─ body limit ─ rate limit ─ timeout   (global)
  → JWT middleware                                                (authenticated group)
  → Authorize(slug, level)                                       (permission-gated route)
  → handler:  bind DTO ────────────── validation error → *AppError(VALIDATION)
  → service:  business rules ──────── rule violation    → *AppError(CONFLICT, FORBIDDEN, …)
  → repository: query ─────────────── driver error      → *AppError(NOT_FOUND, INTERNAL, …)
  ← service maps entity → DTO (drops PasswordHash and every other internal field)
  ← handler renders 200/201 with the DTO
```

The error path is the same path in reverse: whatever layer *knows* what went wrong classifies it,
and every layer above just propagates.

---

## The error model

**Rule: a bare `error` never reaches the transport layer.** Every failure crossing a layer boundary
is an `*AppError` from `internal/shared/errors`, classified at the point of origin:

```go
return nil, errors.NewNotFoundError("user not found", nil)
return nil, errors.NewValidationError("password must be at least 8 characters", nil)
return nil, errors.NewConflictError("email already registered", nil)
return nil, errors.NewInternalError("failed to persist user", err)   // wraps the cause
```

Handlers do not choose status codes. They call one `handleError` helper, which asks the error what
it is:

```go
func handleError(c *gin.Context, err error) {
    var appErr *errors.AppError
    if !errors.As(err, &appErr) {
        appErr = errors.NewInternalError("unexpected error", err)
    }
    c.JSON(appErr.ToHTTPStatus(), appErr.Response())
}
```

The wire format is stable and contains no internal detail:

```json
{ "type": "NOT_FOUND", "error": "user not found", "request_id": "01H..." }
```

**Why this is worth the discipline.** Three properties fall out of it, and none survives handlers
that write their own `c.JSON(500, gin.H{"error": err.Error()})`:

1. **The status mapping is defined once.** Deciding that `CONFLICT` should be 409 rather than 422 is
   a one-line change that applies everywhere.
2. **Client mistakes are not reported as server faults.** Only 5xx is captured by Sentry, so the
   issue feed stays signal. A hardcoded 500 on a validation failure destroys that distinction.
3. **Driver text never reaches a client.** `pq: duplicate key value violates unique constraint
   "users_email_key"` tells an attacker your table and column names. The wrapped cause is logged
   with the request ID; the client gets the classified message.

The `Internal` variant keeps the original error for the log and returns a generic message to the
caller — that asymmetry is the point.

---

## Auth and RBAC

### The token

Login issues an HS256 JWT containing the user ID, role ID, hierarchy level, a **flattened permission
map**, and a permission-version number. Because permissions travel in the token, **authorization
never queries the database** — the check is a map lookup on a struct the middleware already parsed.

`JWTMiddleware` verifies the signature, the issuer and the expiry (all required explicitly — see the
`golang-jwt/jwt/v5` note in [`LIBRARIES.md`](LIBRARIES.md#auth--crypto)), then sets four context
keys. Downstream code type-asserts these exactly:

| Context key | Type |
|---|---|
| `user_id` | `uuid.UUID` |
| `role_id` | `int64` |
| `hierarchy_level` | `int64` |
| `permissions` | `map[string]bool` |

### Hierarchy

**A lower number is more senior**, the way Unix process priority works. `Authorize` grants access
when `userLevel <= requiredLevel` — "you must be at least this senior".

| Role | Level |
|---|---|
| `SUPER_ADMIN` | 1 |
| `ADMIN` | 10 |
| `MANAGER` | 20 |
| `USER` | 100 |

The gaps are intentional: inserting a role between `ADMIN` and `MANAGER` should not require
renumbering every existing role and re-issuing every token.

A route is gated by both a permission slug and a level:

```go
users.GET("/:id", middleware.Authorize("users:read", RoleManager), h.GetByID)
users.DELETE("/:id", middleware.Authorize("users:delete", RoleAdmin), h.Delete)
```

Slugs are `resource:action`. **The slug on the route and the slug in the permission seeder must
match exactly** — a typo fails closed, silently removing access rather than granting it. That is the
safe direction to fail, and it is also why it is easy to miss in review.

`SUPER_ADMIN` (level 1) bypasses the permission map. This is a deliberate break-glass so a
misconfigured permission table cannot lock everyone out of the system that fixes permission tables.
It is also, unavoidably, a single role that can do anything — treat those accounts accordingly.

### Revocation without a database round-trip

Stateless tokens have one classic weakness: they remain valid until they expire, even after you
revoke the user's access. Two gates close that, both reading in-memory state:

- **`TOKEN_STALE`** — every role carries a permission version, bumped whenever its permissions
  change. A token whose version does not match the current one is rejected, so a permission change
  invalidates outstanding tokens immediately.
- **`USER_SUSPENDED`** — suspending a user adds them to an in-memory set checked on every request.

### The scaling limit — read this before you deploy replicas

The permission registry is a **package-level singleton that fx does not manage**. It is process-local
and rebuilt at boot, which is why step 7 of the boot sequence exists.

**Consequence: as written, this design is single-instance.** With two replicas behind a load
balancer, a permission change or a suspension applied on replica A does not propagate to replica B,
and requests routed to B keep succeeding with the old rights until B restarts. Nothing warns you.

Three ways out, in increasing order of effort:

1. **Short `JWT_TTL`** (minutes) so stale rights expire quickly on their own. Cheapest, and often
   enough.
2. **Move the registry to a shared store** (Redis) with a local cache and a short TTL. Revocations
   propagate within the TTL and the hot path stays a memory read.
3. **Publish invalidation events** (Redis pub/sub, NATS) so replicas evict on change. Fastest, most
   moving parts.

This is documented rather than solved because the right answer depends on a deployment the template
cannot know about — but it must be a conscious decision, not a surprise.

---

## Migrations

The schema's source of truth is **GORM struct tags plus `AutoMigrate`**, driven by a version map in
`internal/shared/database/migrations/`, with applied versions recorded in `migration_records`. They
run automatically at boot; there is no separate migrate command to forget.

Two rules:

**Keys must be contiguous from 1.** The runner iterates to `len(migrationMap)`, so a gap both
truncates the run and then panics on a nil function. If you branch and two people both add version
7, one of them renumbers.

**Declare a local anonymous struct inside the migration.** Migrations must describe the schema *as
it was at that version*. If migration 3 references `domain.User` and someone later adds a field to
that struct, migration 3 retroactively changes meaning and a fresh database no longer reproduces the
history:

```go
3: func(db *gorm.DB) error {
    type user struct {
        ID       uuid.UUID `gorm:"type:uuid;primaryKey"`
        Username string    `gorm:"uniqueIndex;not null"`
    }
    return db.Table("users").AutoMigrate(&user{})
},
```

Seeders re-run on every boot and must be idempotent — key them on a natural unique column and
upsert. The administrator seeder skips when `ADMIN_USERNAME`, `ADMIN_EMAIL` or `ADMIN_PASSWORD` is
missing, and **logs a warning when it skips**, because a silent no-op here looks identical to a
successful boot with no way to log in.

---

## Configuration

Everything comes from environment variables, parsed and validated once in `internal/config` into a
typed struct that fx provides to the rest of the graph. Nothing reads `os.Getenv` outside that
package.

For local development, `godotenv` layers three files, later overriding earlier:

```
.env  →  .env.$GO_ENV  →  .env.$GO_ENV.local
```

Shared defaults, per-environment values, machine-local overrides. All are gitignored; only
[`.env.example`](../.env.example) is committed, and it documents every key.

**Invalid configuration is a startup failure, not a runtime surprise.** A missing `DATABASE_URL`, a
`JWT_SECRET` under 32 characters or still set to the placeholder, `CORS_ALLOWED_ORIGINS=*` combined
with credentials — each stops the process with a message naming the key. Booting with a broken
security-relevant setting is worse than not booting.

The loader uses `godotenv.Overload`, which *overwrites* existing process environment variables. That
is what makes the tiers work, and it is a trap for tests — see [`TESTING.md`](TESTING.md#the-trap-the-harness-works-around).

---

## Code style

### Declarations use `var` with an explicit type

This is the project's most visible convention and the one people ask about first, so here is the
full reasoning.

**The rule.** Production code under `internal/` and `cmd/` declares variables with `var` and an
explicit type. Multi-return calls are declared first and assigned on the next line. `range` loop
variables are hoisted.

```go
// Correct
var hashedPassword string
var err error
hashedPassword, err = s.crypt.HashPassword(req.Password)

var user *domain.User = domain.NewUser(req.Email, hashedPassword)

var key string
var value string
for key, value = range headers {
    // ...
}
```

```go
// Rejected
hashedPassword, err := s.crypt.HashPassword(req.Password)
user := domain.NewUser(req.Email, hashedPassword)
for key, value := range headers {
```

**Why.**

1. **The type is visible at the declaration site.** With `:=` you learn a variable's type by finding
   the function it came from and reading *its* signature — possibly in another package. This matters
   most where it is most dangerous: `var count int64 = ...` versus `var count int = ...` is a silent
   overflow difference, and `user := repo.Find(id)` does not tell you whether `user` is a value, a
   pointer, or a `(value, bool)` pair without going to look.

2. **Reviewers see types in the diff.** Code review happens in a browser with no jump-to-definition.
   A diff line `var claims *auth.Claims = parse(token)` can be reviewed on its own; `claims :=
   parse(token)` cannot.

3. **Debugging is more direct.** When you set a breakpoint and inspect a variable, the declaration
   already tells you what you expect to see, so a mismatch between intent and reality is immediately
   obvious rather than something you deduce from the debugger's own inference.

4. **It removes shadowing ambiguity.** `:=` inside an `if` or `for` block quietly creates a *new*
   variable when you meant to assign to the outer one — the single most common source of "my error
   was checked but the outer `err` was nil" bugs. `var` plus `=` makes shadowing something you have
   to write on purpose.

5. **It is a mechanical rule, so it is not a debate.** Conventions that require judgement get
   re-litigated in every review. This one is checkable at a glance.

**The honest counter-argument.** This is *not* idiomatic Go. The standard library uses `:=`
throughout, Go's own style guidance prefers it, and the rule costs you an extra line on every
multi-return call. Verbosity is a real cost and this template pays it deliberately, judging that
explicitness helps more on a codebase many people (and increasingly, coding assistants) read without
an IDE than the brevity does. **If your team disagrees, drop the rule** — nothing in the code
depends on it. It is a style choice, and it is documented here so that it is a *choice* rather than
an inconsistency.

**The exception: test files use idiomatic `:=`.** `test/` and every `_test.go` file are ordinary Go.
Table-driven tests are dominated by short-lived locals whose types are obvious from the literal
next to them, and applying the mandate there adds noise for no clarity. `test/harness` and
`test/fixtures` are test infrastructure and follow the same relaxed rule.

### The rest

- **Pointers for domain entities, DTOs and errors.** `nil` is the "not found" signal, which
  distinguishes "no such row" from "a row that happens to be empty".
- **`uuid.UUID` primary keys.** See [`LIBRARIES.md`](LIBRARIES.md#identity) for why.
- **`snake_case` in the database, `PascalCase` in Go**, with GORM tags kept in parity with the
  migrations.
- **Every file opens with `// File: <path>`.** Small thing; it survives copy-paste into an issue or
  a chat window and tells the reader where the snippet came from.
- **Run `gofmt`/`goimports` on the code you touch.** Both are enforced by `make lint`.

---

## Security decisions baked in

The template ships the corrected version of each of these on purpose. If you are adapting code from
elsewhere, these are the places to check first.

| Decision | Why |
|---|---|
| **Registration ignores any client-supplied role.** The default role is assigned server-side; role changes go through a permission-gated endpoint. | Accepting `role_id` from a registration body is privilege escalation by JSON field — anyone can self-provision an admin account. |
| **Login rejects non-active accounts** before comparing passwords, and returns the same generic message either way. | An account can be disabled but still hold a valid password; and a distinct "account disabled" response is a user-enumeration oracle. |
| **Verification tokens are JSON-serialised, then HMAC-signed** over the exact bytes. | Ad-hoc `key:value|key:value` payloads let a value containing the delimiter smuggle extra fields into a validly signed token. The signature is valid; the parse is attacker-controlled. |
| **Password-reset links carry an opaque single-use token** with a short TTL, never a password hash. | A hash in a URL leaks through referrers, browser history, proxies and server logs — and is offline-crackable. |
| **Only classified `AppError` messages reach clients**; causes are logged with the request ID. | Raw driver text discloses schema, and a hardcoded 500 mislabels client errors. |
| **CORS is an explicit origin allow-list.** `*` with `Allow-Credentials: true` is rejected at startup. | The combination is invalid per the Fetch standard, and where browsers tolerate it, every authenticated endpoint becomes readable by any site. |
| **`JWT_SECRET` is validated at startup** — non-empty, ≥32 characters, not the placeholder. | An unvalidated secret means booting successfully while signing tokens with an empty key, which anyone can forge. |
| **Rate limits on every route, tighter on the auth routes.** | The only thing standing between a public login endpoint and offline-speed credential stuffing. |
| **Server and per-request timeouts are always set.** | Go's `http.Server` has no default timeouts; without them a handful of slow clients exhausts the connection pool. |
| **Body size is capped** (`MAX_REQUEST_BODY_BYTES`). | An unbounded JSON body is a memory-exhaustion primitive. |
| **`TRUSTED_PROXIES` defaults to empty.** | Trusting `X-Forwarded-For` unconditionally lets a client set its own IP and walk past the rate limiter. |

See [`SECURITY.md`](../SECURITY.md) for reporting and for the hardening checklist before you deploy.

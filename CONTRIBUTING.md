# Contributing

Thanks for taking the time. This is a template, so changes are judged on one extra question beyond
"is it correct?" — **does it make the template a better starting point for someone else's project?**
A feature that only makes sense for one application usually belongs in a fork.

Read [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) before your first change. Several rules there are
deliberate and non-obvious, and a change that ignores them tends to fail at startup rather than at
compile time.

- [Getting set up](#getting-set-up)
- [Branching](#branching)
- [Commit messages](#commit-messages)
- [Code style](#code-style)
- [Adding a feature module](#adding-a-feature-module)
- [Database migrations](#database-migrations)
- [Tests](#tests)
- [Before opening a pull request](#before-opening-a-pull-request)
- [Reporting security issues](#reporting-security-issues)

---

## Getting set up

See [Quick start](README.md#quick-start). In short: `cp .env.example .env`, point `DATABASE_URL` at
a PostgreSQL instance, set a real `JWT_SECRET`, then `make dev`.

`make tools` installs the optional local tooling (`air`, `golangci-lint`, `swag`).

---

## Branching

Branch off `master`. Name branches `<type>/<kebab-case-description>`:

```
feat/refresh-tokens
fix/cors-preflight-headers
docs/libraries-rationale
chore/bump-gin
```

Types in use: `feat/` (new capability), `fix/` (bug fix), `docs/` (documentation only),
`chore/` (dependencies, tooling, CI), `refactor/` (no behaviour change).

---

## Commit messages

`Type: Description`, capitalised exactly like this:

```
New: Rate-limit middleware with a tighter bucket on auth routes
Update: Raise the default bcrypt cost to 12
Fix: Reject CORS wildcard when credentials are enabled
Docs: Explain the var-over-:= convention
```

Use `New:`, `Update:`, `Fix:` or `Docs:`. Keep the subject under about 72 characters and use the
body to explain *why* when the reason is not obvious from the diff. Reference an issue with
`Closes #12` on its own line.

---

## Code style

These are enforced, not suggested. They are the most common reason a change needs rework.

**1. Declarations use `var` with an explicit type — never `:=`** in production code under
`internal/` and `cmd/`. Declare first, assign on the next line for multi-return calls, and hoist
`range` variables.

```go
// Correct
var hashedPassword string
var err error
hashedPassword, err = s.crypt.HashPassword(req.Password)

// Rejected
hashedPassword, err := s.crypt.HashPassword(req.Password)
```

The full reasoning — and the honest counter-argument — is in
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md#declarations-use-var-with-an-explicit-type).
**Test files are exempt** and use idiomatic `:=`.

**2. Never return a bare `error` to the transport layer.** Return `*AppError` from
`internal/shared/errors`, classified at the point of origin. Handlers must not write their own
status codes — they call `appErr.ToHTTPStatus()` through the shared `handleError` helper.

```go
return nil, errors.NewValidationError("username contains unsupported characters", nil)
```

Never put a driver error, a wrapped cause or a stack trace in a client response. Log the cause;
return the classification.

**3. `uuid.UUID` primary keys**, `snake_case` in the database, `PascalCase` in Go, and GORM tags kept
in parity with the migrations.

**4. Return pointers** for domain entities, DTOs and errors. `nil` is the "not found" signal.

**5. Entities never leave the service layer.** Map to a DTO. This is what keeps `PasswordHash` and
every other internal field out of responses, and it needs to be visible in the diff.

**6. Keep layer boundaries.** `gorm` and `pgx` appear only in `infra/`. `gin` appears only in `api/`
and `shared/middleware`. A module never imports another module's `api` or `infra`.

**7. Every file opens with `// File: <path>`.**

Run `gofmt` and `goimports` on the code you touch — `make lint` enforces both.

---

## Adding a feature module

Create `internal/modules/<name>/` with the four layers, then wire it. The steps that fail *silently*
if you skip them are marked.

1. **Write the six files** — `domain/<entity>_model.go`, `domain/<entity>_repository.go`,
   `infra/<entity>_repository.go`, `service/<entity>_service.go`, `service/<entity>_dto.go`,
   `api/<entity>_handler.go`.

2. **Add `fx.Provide` lines to the module's `module.go`** — repository, service, handler, and the
   `fx.Annotate(..., fx.ResultTags(`group:"models"`))` stanza for each persisted entity.
   ⚠️ **Omitting the models stanza means the table is never created.** The app boots fine and fails
   on the first query.

3. **Keep the repository constructor's return type as the interface**
   (`func NewGormXRepository(db *gorm.DB) domain.XRepository`). fx binds on the declared return
   type; returning the concrete type breaks every consumer's resolution.

4. **Add the handler to the route-registration function's parameters** in `cmd/api`.
   ⚠️ **fx builds lazily — anything unreachable from there is never constructed**, so the endpoints
   will 404 despite the code compiling.

5. **Register the routes** in the right tier (public / authenticated / permission-gated) with
   `middleware.Authorize(slug, level)`.

6. **Seed the permission slugs.** ⚠️ Route slugs and seeded slugs must match *exactly*. A mismatch
   fails closed and silently removes access.

7. **Annotate the handlers** for Swagger and run `make swagger`. DTOs must live in `service/` to
   resolve as `service.XxxDTO`.

8. **Add tests** — see below — and a fixture builder if other tests will need the entity.

---

## Database migrations

Append the next integer key to the map in `internal/shared/database/migrations/`.

⚠️ **Keys must be contiguous starting at 1.** The runner iterates to `len(migrationMap)`, so a gap
truncates the run and then panics on a nil function. If two branches both add version 7, one
renumbers when rebasing.

Declare a **local anonymous struct** inside the migration rather than referencing the domain model,
so the migration stays frozen against later changes to that model. Migrations run automatically at
boot and are recorded in `migration_records`.

Seeders re-run on every boot and must be idempotent — key on a natural unique column and upsert.

---

## Tests

Layered; the full architecture is in [`docs/TESTING.md`](docs/TESTING.md).

```bash
make test              # unit + package tests, no database required
make test-db-up        # disposable PostgreSQL on :55433
make test-integration  # real fx graph against a real database
make test-e2e          # the compiled server, driven over HTTP
make test-db-down
```

Where a test goes:

- **Service logic** → `test/unit`, using the generated mock from `internal/mocks`
  (run `make mocks` after changing a repository interface).
- **SQL, migrations, seeders or the DI wiring** → `test/integration`, via `harness.App`.
- **Routes, middleware or auth** → `test/e2e`, via `harness.StartServer`.

Assert on the error *type* rather than the message string, and give every security fix a permanent
regression test.

---

## Before opening a pull request

```bash
make check       # go vet + go test -race
make lint        # golangci-lint, using the committed config
make vuln        # govulncheck
```

Then the checklist in the PR template. The ones people miss most often:

- New configuration keys documented in `.env.example` **with a placeholder value**.
- `make swagger` re-run if you changed a handler annotation.
- `CHANGELOG.md` updated for anything user-visible.
- No secrets. Every `.env*` file except `.env.example` is gitignored — keep it that way, and never
  put a real DSN, SMTP credential or personal email address anywhere in the tree.

CI runs build, vet, test, integration, e2e, govulncheck and lint on every push.

---

## Reporting security issues

Do not open a public issue or a PR containing a proof of concept — see [`SECURITY.md`](SECURITY.md).

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

### Planned

| Phase | Contents |
|---|---|
| 2 | Go module and foundations — `go.mod`, tiered config, zerolog logger, the `AppError` kernel |
| 3 | Database — GORM connector, versioned migrations, seeders, fx value groups |
| 4 | Users and auth — the user module, JWT, bcrypt, the HMAC token codec |
| 5 | RBAC — roles, permissions, the `Authorize` middleware |
| 6 | Email and messages — SMTP mailer with embedded templates, the notifications module |
| 7 | Middleware and health — CORS allow-list, request ID, rate limiting, timeouts, probes |
| 8 | Tests and tooling — harness, fixtures, mocks, the three suites, Docker, Swagger |

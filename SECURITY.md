# Security Policy

This is a template. Code copied from it ends up in real services holding real user data, so a flaw
here propagates. Security reports are treated as high priority.

---

## Reporting a vulnerability

**Do not open a public issue, a pull request, or a discussion for a security problem, and do not
publish a proof of concept before a fix is available.**

Report it privately through GitHub:

> **[Open a private security advisory](https://github.com/DevanshSharmaT-T/Go-Gin-Template/security/advisories/new)**
> — repository → **Security** tab → **Report a vulnerability**

That channel is private to the maintainers, supports attachments and follow-up discussion, and
issues a CVE if one is warranted. GitHub documents the reporter's side
[here](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability).

Please include:

- what you found, and which file, endpoint or configuration it affects;
- how to reproduce it, ideally as a failing test or a `curl` sequence;
- the impact you believe it has, and any preconditions (an authenticated session, a specific
  configuration, a particular Go or PostgreSQL version).

**What to expect.** This is a maintained-in-spare-time open-source project: you will get an
acknowledgement as soon as a maintainer sees the report, and updates as the fix progresses. There is
no funded SLA. Credit in the advisory and the changelog unless you prefer otherwise.

---

## Scope

**In scope** — anything in this repository that would be a flaw in a service built from it:

- authentication, session and token handling;
- the RBAC model and the `Authorize` middleware;
- token generation, verification and revocation;
- data exposure through endpoints or error responses;
- the middleware set (CORS, rate limiting, timeouts, body limits, proxy trust);
- insecure defaults in `.env.example`, the `Makefile` or CI;
- vulnerable dependencies that are reachable from this code.

**Out of scope**

- Vulnerabilities in a downstream project that has modified the template.
- Findings that require an already-compromised host, database or developer machine.
- Missing hardening in a dependency, reported without a path from this code to it — report those
  upstream.
- Anything whose only impact is on a deployment you control and can fix by configuration, unless the
  default configuration is what leads people there.

---

## Security decisions in the template

These are deliberate, and each exists because the naive version is a real vulnerability. If you are
adapting the code, keep them.

| | |
|---|---|
| **Registration never accepts a client-supplied role.** | Otherwise anyone can self-provision an administrator by adding a field to the request body. |
| **Login rejects non-active accounts** and returns the same generic message for every failure. | A disabled account can still hold a valid password, and a distinct "account disabled" response is a user-enumeration oracle. |
| **Verification tokens are JSON-serialised before HMAC signing.** | Ad-hoc `key:value|key:value` payloads let a value containing the delimiter smuggle extra fields into a validly signed token — the signature verifies and the parse is attacker-controlled. |
| **Password-reset links carry an opaque, single-use, short-TTL token** — never a password hash. | Hashes in URLs leak through referrers, browser history, proxies and logs, and are crackable offline. |
| **Only classified `AppError` messages reach clients.** | Raw driver text discloses schema; a hardcoded 500 mislabels client errors and poisons alerting. |
| **CORS is an exact-match allow-list.** `*` with `Allow-Credentials: true` is rejected at startup. | The combination is invalid per the Fetch standard, and where it is tolerated every authenticated endpoint becomes readable by any site. |
| **`JWT_SECRET` is validated at startup** — non-empty, at least 32 characters, and not the placeholder. | An unvalidated secret means booting happily while signing tokens with an empty key, which anyone can forge. |
| **Token signing keys are domain-separated.** The verification-token key is derived from `JWT_SECRET` via HKDF with a distinct label, or set independently. | Reusing one key across two token formats turns a weakness in either into a weakness in both. |
| **Rate limiting on every route, tighter on auth routes.** | The only thing between a public login endpoint and offline-speed credential stuffing. |
| **Server and per-request timeouts are always set; request bodies are capped.** | Go's `http.Server` has no default timeouts, and an unbounded body is a memory-exhaustion primitive. |
| **`TRUSTED_PROXIES` defaults to empty.** | Trusting `X-Forwarded-For` unconditionally lets a client choose its own IP and walk past the rate limiter. |
| **Passwords are bcrypt-hashed with a configurable cost**, never logged, never returned. | — |

---

## Before you deploy

The template is hardened by default, but a few things only you can do.

- [ ] **Generate real secrets.** `openssl rand -base64 48` for `JWT_SECRET`, and for `TOKEN_SECRET`
      if you set it independently. Never reuse a secret between environments.
- [ ] **Never commit a `.env` file.** All `.env*` files are gitignored except `.env.example`, which
      contains placeholders only. Inject configuration as real environment variables in production
      rather than baking files into an image.
- [ ] **Set `CORS_ALLOWED_ORIGINS`** to your actual front-end origins, with scheme and port.
- [ ] **Terminate TLS** in front of the service, and use `sslmode=require` (or stricter) in
      `DATABASE_URL`.
- [ ] **Set `TRUSTED_PROXIES`** to your load balancer's CIDR — and *only* if there is one.
- [ ] **Change the seeded administrator password** immediately after the first boot, or omit the
      `ADMIN_*` variables and create the account another way.
- [ ] **Give the database role the least privilege it needs.** It needs `CREATE EXTENSION` once, at
      first boot; it does not need superuser afterwards.
- [ ] **Set `LOG_FORMAT=json`, an appropriate `LOG_LEVEL`, and ship the logs somewhere.** Debug-level
      logs are more verbose than you want in production.
- [ ] **Read [the scaling limit](docs/ARCHITECTURE.md#the-scaling-limit--read-this-before-you-deploy-replicas)
      before running more than one replica.** The permission registry and the rate limiter are both
      in-process, so revocations and limits do not propagate between instances.
- [ ] **Run `make vuln` in your release pipeline**, and keep the Go toolchain patched — some
      vulnerabilities are fixed only by a toolchain update, not by a `go.mod` bump.

---

## Dependency scanning

```bash
make vuln     # go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

`govulncheck` reports vulnerabilities that are actually *reachable* from this code, not merely
present in the dependency graph, which keeps the signal high. CI runs it on every push.

---

## Supported versions

Only the current default branch receives fixes. There are no tagged releases or backports yet; when
tagged releases exist, this section will state the support window.

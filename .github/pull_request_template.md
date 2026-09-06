## What changed

<!-- What this does and why it is needed. Link an issue with "Closes #12" if there is one. -->

## Type

- [ ] `New:` — new capability
- [ ] `Update:` — change to existing behaviour
- [ ] `Fix:` — bug fix
- [ ] `Docs:` — documentation or chore

## Checklist

- [ ] `make check` passes (`go vet` + `go test -race`)
- [ ] `make lint` passes
- [ ] No `:=` in production code — declarations use `var` with an explicit type (tests are exempt)
- [ ] Errors are returned as `*AppError`; handlers use `appErr.ToHTTPStatus()` rather than
      hardcoded status codes, and no driver text reaches a client
- [ ] Layer boundaries respected — `gorm`/`pgx` only in `infra/`, `gin` only in `api/` and
      `shared/middleware`
- [ ] New config keys documented in `.env.example` with a **placeholder** value
- [ ] No secrets, credentials, real DSNs or personal email addresses anywhere in the diff
- [ ] New module: `group:"models"` stanza added, handler wired into route registration, permission
      slugs seeded with slugs that match the routes exactly
- [ ] New migration key is contiguous with the previous version, and uses a local anonymous struct
- [ ] Tests added at the right layer (unit / integration / e2e); security fixes have a permanent
      regression test
- [ ] `make swagger` re-run if handler annotations changed
- [ ] `CHANGELOG.md` updated if the change is user-visible

## Notes for reviewers

<!-- Anything surprising, deliberately out of scope, or worth a closer look. -->

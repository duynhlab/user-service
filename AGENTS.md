# AGENTS.md

Agent guide for `user-service`. Keep changes small, verified, and consistent
with the patterns below.

## Authority and scope

This repository implements the service. It does **not** define the contract.

- **Canonical contract:** [`homelab/docs/api/user.md`](https://github.com/duynhlab/homelab/blob/main/docs/api/user.md)
- **Shared API rules:** [`homelab/docs/api/api.md`](https://github.com/duynhlab/homelab/blob/main/docs/api/api.md)

Implement against those files. When this repository and the contract disagree,
**stop and classify the mismatch** using
[Resolving a mismatch](https://github.com/duynhlab/homelab/blob/main/docs/api/README.md#resolving-a-mismatch)
before changing either side. One class — an implementation that violates the
intended contract — **blocks the release tag**.

No route, RPC, payload or error inventory belongs in this file. Manifests,
gateway routing, NetworkPolicy and platform observability belong to
[duynhlab/homelab](https://github.com/duynhlab/homelab).

## Contribution workflow

- Never commit or push to `main`. Branch, then open a PR.
- Branch names: `feat/…`, `fix/…`, `docs/…`, `chore/…`, `refactor/…`.
- One logical change per PR. Squash-merge.
- Commit subject: ≤ 50 chars, imperative mood, capitalised, no trailing period
  (`Add profile upsert path`, not `Added` / `Adds`).
- Commit body (only if non-trivial): wrap at 72 chars, explain *what* and *why*.
- Do **not** add attribution trailers (`Signed-off-by`, `Co-authored-by`,
  `Generated-by`, …), issue references (`Fixes #123`), or `@`-mentions.
- CI (`.github/workflows/check.yml`) runs `pr-checks`, `go-check` (test + lint),
  gitleaks, sonar and notify on every PR. Green is required.
- Before pushing or opening a PR, verify Sonar new-code coverage ≥ 80%: run
  `go test -race -coverprofile=coverage.out ./...` and confirm changed lines
  are covered, including **both** branches of any new conditional.
  `**/cmd/**`, `**/db/migrations/**`, `**/internal/core/repository/**` are
  coverage-excluded; everything else counts.

## Build, test, lint

These are the commands CI runs, so a green local run means a green pipeline.

```bash
go build ./...
go vet ./...
go test -race ./...
go test -tags=integration ./internal/core/repository/...   # needs Docker (testcontainers)
golangci-lint run
```

Local development against an unreleased `pkg`: `pkg` is one module per package,
so its root has no `go.mod` and a single `replace github.com/duynhlab/pkg` can no
longer resolve. Use one commented `replace` line per module — the trailer in
`go.mod` shows the shape, and
[`docs/api/pkg.md`](https://github.com/duynhlab/homelab/blob/main/docs/api/pkg.md)
explains why.

## Architecture boundaries

**3-layer, dependencies flow one way only: transport → logic → core.**

- **Transport** — `internal/web/v1/` validates, maps and delegates. It may not
  touch the database or hold business rules.
- **Logic** — `internal/logic/v1/` holds the rules and calls the repository
  interface. No SQL, no gin types.
- **Core** — `internal/core/` owns the domain model, the repository interface and
  the Postgres implementation. It imports nothing from transport or logic.

HTTP is the only transport. Observability is wired once through
`github.com/duynhlab/pkg/obsx`; the pool comes from `github.com/duynhlab/pkg/dbx`;
responses use the shared `github.com/duynhlab/pkg/httpx` envelope; JWTs are
verified by `github.com/duynhlab/pkg/authmw`.

## Invariants

Rules an implementer can violate at the keyboard.

- **Owner scoping is structural: never add a private route that takes an id.**
  Private handlers resolve the profile from the JWT subject alone, and return 401
  when that context value is empty. This is what makes a cross-user read
  *unrepresentable* rather than merely forbidden — a `/:id` private route would
  hand that property away.
- **Identity fields come from the verified token, never the request body.** The
  update request carries only name and phone; there is no id field to trust.
- **Do not hand-roll JWT parsing.** Use `authmw`. Verifier construction failure
  is fatal on purpose, and the verifier refreshes JWKS in the background rather
  than blocking on an unreachable endpoint, which is why building it at startup
  is safe.
- **Never query auth's tables.** This service owns only `user_profiles`; the
  authoritative `users` table lives in another service across a cluster boundary
  with no foreign key. Resolve the display name locally and leave identity fields
  empty rather than reaching across.
- **The public view is a deliberate minimal projection** — id and name only. It
  omits email and everything else sensitive. Adding a field to it is a contract
  change, not a convenience.
- **The internal create requires a caller-supplied user id and never mints one.**
  Identity ids belong to auth-service; synthesising one here would create a
  profile no account can claim.
- **Partial update means empty preserves and non-empty replaces**, expressed as
  `COALESCE(NULLIF(…))` in a single statement. Switching to an unconditional
  `SET` would blank a name on a phone-only update.
- **The profile upsert is update-then-insert and is not atomic.** Concurrent
  first-writes for the same user can collide on the unique index. Do not build on
  an idempotency guarantee that is not there — the contract records this gap.
- **Pooler-safe database settings live in `pkg/dbx`.** Simple protocol and
  disabled statement caches are required by the pooler. One DSN serves the app
  and migrations so both connect identically; pool sizing stays off the DSN
  because the driver migrations use rejects `pool_*` parameters.
- **`seed` is development-only** and refuses production. It is invoked explicitly
  — never from `migrate` or the serve path — and must not use golang-migrate:
  seeds are idempotent `ON CONFLICT` statements and must not share the
  `schema_migrations` version table.
- **Graceful-shutdown ordering is load-bearing:** flag not-ready → readiness drain
  delay → HTTP shutdown → pool close → OTel shutdown last, so pending spans,
  metrics and logs still flush.
- **Metric labels are bounded enums — no user ids, usernames, emails or other
  PII.** Persistence failures are deliberately not counted; they surface as
  database spans instead.
- **Probe suppression is one contract across logs and traces.** Successful
  `/health` and `/ready` requests are excluded from spans *and* access logs
  through the same skip list; a **failing** probe is still recorded. 4xx logs at
  warn, 5xx at error.
- **The logged `trace_id` must be the active span's, or absent.** A synthesised
  id looks joinable while joining to nothing, which is worse mid-incident than no
  field at all. The generated fallback belongs only on the response header, which
  is a separate client contract.
- **Handlers must not open their own span.** otelgin already opened the server
  span and owns its lifecycle; annotate it, never end it.
- **Raw binder and validator errors must never reach the client** — sanitise
  them, for security and for the message the caller actually needs.

## Repository map

- `cmd/main.go` — bootstrap, subcommand dispatch, routes, graceful shutdown
- `config/config.go` — env config, `Validate()`, `BuildDSN()`
- `internal/web/v1/` — handlers, the public projection, and validation sanitising
- `internal/logic/v1/` — business rules, span annotation, metrics
- `internal/core/` — pool wiring, domain model, repository interface and Postgres implementation
- `db/migrations/` — forward-only golang-migrate SQL, embedded
- `db/seed/` — development-only demo seed, embedded
- `middleware/` — tracing and logging only

## Gotchas

- Kyverno admission rejects a workload image tagged `:latest` or unpinned. The
  published image is `ghcr.io/duynhlab/user-service/user-service:<tag>` — the
  repository path repeats, and the tag carries no `v` prefix. There is no
  separate migration image; the init container reuses the app image with
  `args: ["migrate"]`.
- Metrics leave over OTLP. There is no `/metrics` endpoint and nothing scrapes
  this service.
- The JWKS default is the `/auth/v1/public/auth/jwks` path. The shorter
  `/auth/v1/public/jwks` is a deprecated alias; do not copy it into config or
  docs.

## API change synchronization

An API change is not done when the code compiles.

- The contract in homelab and this repository move **together** — same change,
  and either the same PR pair or an immediate follow-up.
- Behaviour that is designed but not deployed is marked **`Planned`** in the
  contract; it is never described as current.
- A material mismatch between the contract and this implementation **blocks the
  release tag** until it is reconciled or explicitly accepted.

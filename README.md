# user-service

Customer profiles: the name, phone and address attached to an account, and the
minimal public view of them.

## Responsibilities

- **Owns:** the `user_profiles` row for a given user — first and last name,
  phone, address — and the public display-name projection derived from it.
- **Does not own:** identity. Credentials, username and email uniqueness, the
  authoritative `users` table and token issuance all belong to `auth-service`.
  The profile references a user id across that boundary with no foreign key.

## Tech

| Area | Technology |
|------|------------|
| Runtime | Go 1.27 |
| Transports | HTTP only — no gRPC server, no client, no worker |
| Data | PostgreSQL — one table, `user_profiles` |
| Platform libraries | `authmw`, `dbx`, `httpx`, `logger/zapx`, `migratex`, `obsx` |

## API

- **Canonical contract:** [`homelab/docs/api/user.md`](https://github.com/duynhlab/homelab/blob/main/docs/api/user.md)
- **Shared conventions:** [`homelab/docs/api/api.md`](https://github.com/duynhlab/homelab/blob/main/docs/api/api.md)
- **Surfaces:** a public read of one profile plus a JWT-protected read and update
  of *your own* profile. There is no create route — profiles arrive just-in-time
  from the verified token (see Gotchas). HTTP `:8080` also carries `/health` and
  `/ready`.

Routes, payloads and error codes live in the contract, so there is one place to
change when they change.

## Run locally

Prefer the homelab **local-stack** — the private routes need a signed token, so
auth-service has to be running.

Standalone you need PostgreSQL reachable through the `DB_*` variables and three
roles: `user_owner` (owns the schema, cannot log in), `user_migrator` (logs in,
may only `SET ROLE user_owner`) and `user_runtime` (serves traffic, CRUD only).
`migrate` and `seed` log in as the migrator and need
`DB_MIGRATION_ROLE=user_owner`; they refuse to run without it, and they must
reach PostgreSQL directly, not through a transaction pooler. The app logs in as
`user_runtime`. `user_owner` must own the `user` database: on PostgreSQL 15+
that is what lets it create objects in the `public` schema.

```bash
DB_USER=user_migrator DB_MIGRATION_ROLE=user_owner go run cmd/main.go migrate
DB_USER=user_migrator DB_MIGRATION_ROLE=user_owner go run cmd/main.go seed   # development only
DB_USER=user_runtime go run cmd/main.go   # serve HTTP :8080
```

## Verify

The commands CI runs, so a green local run means a green pipeline:

```bash
go build ./...
go test -race ./...
go test -tags=integration ./internal/core/repository/...   # needs Docker (testcontainers)
golangci-lint run
```

## Docs

- [Canonical contract](https://github.com/duynhlab/homelab/blob/main/docs/api/user.md)
- [local-stack guide](https://github.com/duynhlab/homelab/blob/main/local-stack/README.md)

## License

MIT

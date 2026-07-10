# user-service

User management microservice for profiles and account operations.

## Features

- User profile management (private read/update, JWT-scoped; partial update preserves unset fields)
- Public minimal profile view (`id` + `name` — no PII)
- Internal profile create (reserved — no in-cluster caller today)

## API Endpoints

All routes follow Variant A naming — single path for browser and in-cluster callers. See [homelab naming convention](https://github.com/duynhlab/homelab/blob/main/docs/api/api-naming-convention.md).

| Method | Path | Audience |
|--------|------|----------|
| `GET` | `/user/v1/public/users/:id` | public |
| `GET` | `/user/v1/private/users/profile` | private |
| `PUT` | `/user/v1/private/users/profile` | private |
| `POST` | `/user/v1/internal/users` | internal (auth-service during registration; in-cluster only) |

Infrastructure endpoints: `GET /health`, `GET /ready` (503 while draining), `GET /metrics`.

## Authentication

`private` routes are guarded by the shared `github.com/duynhlab/pkg/authmw` middleware. The middleware verifies the request's `Authorization` bearer token **locally** as an RS256 JWT against auth-service's JWKS (cached, refreshed in the background) — no per-request call to auth-service and no gRPC fallback. Configuration: `AUTH_JWKS_URL` (default `http://auth.auth.svc.cluster.local:8080/auth/v1/public/jwks`), `JWT_ISSUER`, `JWT_AUDIENCE`. The middleware fails closed: missing/invalid/expired token → 401. On success it populates `user_id`/`username`/`email` in the request context for the handlers.

## Observability

- **Tracing**: OpenTelemetry → OTel Collector (OTLP HTTP). Sampling via `OTEL_SAMPLE_RATE` (default 0.1).
- **Metrics**: a single `/metrics` endpoint (Prometheus). HTTP RED metrics (`request_duration_seconds`, `requests_in_flight`, `request_size_bytes`, `response_size_bytes`) come from the in-repo Prometheus middleware. `obsx.SetupMetrics()` additionally bridges the OpenTelemetry meter provider into the same default Prometheus registry, so any OTel-instrumented metrics surface on the **same** `/metrics` — no separate port. One platform ServiceMonitor scrapes everything.
- **Logging**: structured Zap. The logging middleware resolves `trace_id` via `obsx.TraceIDFromContext` (the active span's trace ID), so log lines and traces share the same ID; it falls back to the `traceparent`/`X-Trace-ID` headers or a generated ID only when no span is present.
- **Middleware chain** (order matters): `tracing → logging → metrics`.
- **Profiling**: Pyroscope continuous profiling (toggle via `PROFILING_ENABLED`).

## Tech Stack

- Go 1.26 + Gin framework
- PostgreSQL via pgx/v5 (supporting-db cluster, PgBouncer transaction pooling)
- Local RS256 JWT verification against auth-service's JWKS (`pkg/authmw`)
- OpenTelemetry tracing, Prometheus metrics, Zap logging, Pyroscope profiling (`pkg/obsx`)

## Development

### Prerequisites

- Go 1.26+
- [golangci-lint](https://golangci-lint.run/welcome/install/) v2+
- Docker (only for the integration tests — see [Testing](#testing))

### Local Development

```bash
# Install dependencies
go mod tidy
go mod download

# Build
go build ./...

# Test
go test ./...

# Lint (must pass before PR merge)
golangci-lint run --timeout=10m

# Run locally (requires .env or env vars)
go run cmd/main.go
```

### Testing

Unit tests use the stdlib `testing` package with hand-written mocks and table-driven
subtests (no testify/gomock). The **repository layer** is covered by **integration tests**
against a real PostgreSQL via [testcontainers](https://golang.testcontainers.org/).

```bash
# Unit tests (no Docker)
go test ./...

# With coverage (as CI runs it)
go test -race -coverprofile=coverage.out ./...

# Integration tests — repository layer, real Postgres (needs a running Docker daemon)
go test -tags=integration ./internal/core/repository/...
```

Integration tests are build-tagged `//go:build integration`, so the default `go test ./...`
skips them and the service binary never links testcontainers. CI runs both jobs and merges
their coverage into SonarCloud (gate: ≥ 80% on new code).

### Pre-push Checklist

```bash
go build ./... && \
  go test ./... && \
  go test -tags=integration ./internal/core/repository/... && \
  golangci-lint run --timeout=10m
```

## License

MIT

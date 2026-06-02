# user-service

> AI Agent context for understanding this repository

## 📋 Overview

User management microservice. Handles user profiles and account operations.

Module path: `github.com/duynhlab/user-service`.

It is a gRPC **client** of `auth-service`: the shared `pkg/authmw` middleware
validates each request's bearer token by calling `auth.v1.AuthService/GetMe`
over gRPC (target `AUTH_GRPC_ADDR`). gRPC is the official east-west transport;
this service exposes no gRPC server, only the HTTP API below.

## 🏗️ Architecture Guidelines

### 3-Layer Architecture

| Layer | Location | Responsibility |
|-------|----------|----------------|
| **Web** | `internal/web/v1/handler.go` | HTTP handling, validation, error translation |
| **Logic** | `internal/logic/v1/service.go` | Business rules (❌ NO SQL) |
| **Core** | `internal/core/` | Domain models (`core/domain/`), repository interface + impl (`core/repository/psql/`), DB pool (`core/database.go`) |

### 3-Layer Coding Rules

**CRITICAL**: Strict layer boundaries. Violations will be rejected in code review.

#### Layer Boundaries

| Layer | Location | ALLOWED | FORBIDDEN |
|-------|----------|---------|-----------|
| **Web** | `internal/web/v1/` | HTTP handling, JSON binding, DTO mapping, call Logic, aggregation | SQL queries, direct DB access, business rules |
| **Logic** | `internal/logic/v1/` | Business rules, call repository interfaces, domain errors | SQL queries, `database.GetPool()`, HTTP handling, `*gin.Context` |
| **Core** | `internal/core/` | Domain models, repository implementations, SQL queries, DB connection | HTTP handling, business orchestration |

#### Dependency Direction

```
Web -> Logic -> Core (one-way only, never reverse)
```

- Web imports Logic and Core/domain
- Logic imports Core/domain and Core/repository interfaces
- Core imports nothing from Web or Logic

#### DO

- Put HTTP handlers, request validation, error-to-status mapping in `web/`
- Put business rules, orchestration, transaction logic in `logic/`
- Put SQL queries in `core/repository/` implementations
- Use repository interfaces (defined in `core/domain/`) for data access in Logic layer
- Use dependency injection (constructor parameters) for all service dependencies

#### DO NOT

- Write SQL or call `database.GetPool()` in Logic layer
- Import `gin` or handle HTTP in Logic layer
- Put business rules in Web layer (Web only translates and delegates)
- Call Logic functions directly from another service (use HTTP aggregation in Web layer)
- Skip the Logic layer (Web must not call Core/repository directly)

### Directory Structure

```
user-service/
├── cmd/main.go
├── config/config.go
├── db/migrations/sql/
├── internal/
│   ├── core/
│   │   ├── database.go          # pgxpool connect + global GetPool()
│   │   ├── domain/             # models, errors, UserRepository interface
│   │   └── repository/psql/    # PostgreSQL UserRepository implementation
│   ├── logic/v1/service.go
│   └── web/v1/handler.go
├── middleware/                  # tracing, logging, prometheus, profiling, resource
└── Dockerfile
```

### Wiring (`cmd/main.go`)

```
userRepo    := psql.NewUserRepository()        // no args; uses core.GetPool()
userService := logicv1.NewUserService(userRepo) // repo injected via constructor
userHandler := webv1.NewUserHandler(userService)
authConn, _ := grpcx.Dial(cfg.AuthGRPCAddr)     // gRPC client → auth-service
authClient  := authv1.NewAuthServiceClient(authConn)
```

The `UserService` receives its repository by constructor injection. The
repository implementation (`core/repository/psql`) currently reaches the
connection pool through the package-global `database.GetPool()` rather than a
pool injected into its constructor — keep that pattern unless explicitly asked
to refactor it. `GetUser` is still a stub (returns synthesized data; `id ==
"999"` yields `ErrUserNotFound`) because user-service does not own the `users`
table; the profile read/write paths (`user_profiles`) hit real SQL.

## 🛠️ Development Workflow

### Code Quality

**MANDATORY**: All code changes MUST pass lint before committing.

- Linter: `golangci-lint` v2+ with `.golangci.yml` config (60+ linters enabled)
- Zero tolerance: PRs with lint errors will NOT be merged
- CI enforces: `go-check` job runs lint on every PR

#### Commands (run in order)

```bash
go mod tidy              # Clean dependencies
go build ./...           # Verify compilation
go test ./...            # Run tests
golangci-lint run --timeout=10m  # Lint (MUST pass)
```

#### Pre-commit One-liner

```bash
go build ./... && go test ./... && golangci-lint run --timeout=10m
```

### Common Lint Fixes

- `perfsprint`: Use `errors.New()` instead of `fmt.Errorf()` when no format verbs
- `nosprintfhostport`: Use `net.JoinHostPort()` instead of `fmt.Sprintf("%s:%s", host, port)`
- `errcheck`: Always check error returns (or explicitly `_ = fn()`)
- `goconst`: Extract repeated string literals to constants
- `gocognit`: Extract helper functions to reduce complexity
- `noctx`: Use `http.NewRequestWithContext()` instead of `http.NewRequest()`

## 🔧 Tech Stack

| Component | Technology |
|-----------|------------|
| Language | Go 1.26 |
| Framework | Gin |
| Database | PostgreSQL via pgx/v5 (`pgxpool`) |
| Auth (east-west) | gRPC client of auth-service via `pkg/grpcx` + `pkg/authmw` |
| Logging | Zap (structured, trace-correlated) |
| Tracing | OpenTelemetry (OTLP HTTP → OTel Collector) |
| Metrics | Prometheus (HTTP RED in-repo + gRPC client RED via `pkg/obsx`) |
| Profiling | Pyroscope |
| Shared libs | `github.com/duynhlab/pkg` (`authmw`, `grpcx`, `obsx`, generated `proto/auth/v1`) |

### Observability (single `/metrics`)

The process exposes ONE Prometheus endpoint at `/metrics`. HTTP RED metrics
(`request_duration_seconds`, `requests_in_flight`, `request_size_bytes`,
`response_size_bytes`) are recorded by `middleware/prometheus.go`. In `main()`,
`obsx.SetupMetrics()` installs a global OpenTelemetry meter provider backed by a
Prometheus exporter on the **default** registry, so the gRPC client RED metrics
(`rpc_client_*`) from the otelgrpc handler in `pkg/grpcx` land on the **same**
`/metrics` — there is no separate metrics port (a gRPC server would own `:9090`,
so HTTP can't be served there). One platform ServiceMonitor scrapes it.

Logging middleware derives `trace_id` from `obsx.TraceIDFromContext(ctx)` (the
active span's ID) so logs and traces correlate; it only falls back to the
`traceparent`/`X-Trace-ID` headers or a generated ID when no span exists.

**Middleware order (do not reorder):** `TracingMiddleware → LoggingMiddleware →
PrometheusMiddleware`. Tracing must run first so logging/metrics see the span.

### gRPC auth middleware (`pkg/authmw`)

`private` routes use `authmw.Middleware(authClient)`. It forwards the incoming
`Authorization` header to `auth.v1.AuthService/GetMe` over gRPC and **fails
closed**: no header → 401, `Unauthenticated` → 401, any other error (auth
unreachable, internal) → 503. On success it sets `user_id`, `username`, `email`
in the gin context — handlers read these, never parse JWTs themselves. Do not
reintroduce per-service token parsing; the shared middleware is the single
source of fail-closed behaviour.

## 🏗️ Infrastructure Details

### Database

| Component | Value |
|-----------|-------|
| **Cluster** | supporting-db (Zalando Postgres Operator) |
| **PostgreSQL** | 16 |
| **HA** | Single instance |
| **Pooler** | PgBouncer Sidecar |
| **Endpoint** | `supporting-db-pooler.user.svc.cluster.local:5432` |
| **Pool Mode** | Transaction |
| **Shared DB** | Yes (with notification, shipping services) |

### Graceful Shutdown

**VictoriaMetrics Pattern:**
1. `/ready` → 503 when `isShuttingDown = true`
2. Sleep `READINESS_DRAIN_DELAY` (5s)
3. Sequential: HTTP → Database → Tracer

## 🔌 API Reference

Routes are mounted directly at `/{service}/v1/{audience}/…` (Variant A — single URL shape across browser and in-cluster callers). Kong is pure pass-through for `public`/`private`; `internal` is reachable only via service DNS.

| Method | Path | Audience | Description |
|--------|------|----------|-------------|
| `GET` | `/user/v1/public/users/:id` | public | Get user by ID (no JWT required today — consider adding) |
| `GET` | `/user/v1/private/users/profile` | private | Get current user's profile |
| `PUT` | `/user/v1/private/users/profile` | private | Update current user's profile |
| `POST` | `/user/v1/internal/users` | internal | Create new user — called by `auth-service` during registration via `http://user.user.svc.cluster.local:8080` |

Full convention + inventory: [`homelab/docs/api/api-naming-convention.md`](https://github.com/duynhlab/homelab/blob/main/docs/api/api-naming-convention.md).

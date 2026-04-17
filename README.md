# user-service

User management microservice for profiles and account operations.

## Features

- User profile management
- Account operations
- User search

## API Endpoints

> **Browser callers** hit `https://gateway.duynhne.me/user/v1/{public,private}/users/…`; Kong rewrites to the cluster paths below. `POST /api/v1/users` stays internal (not on the gateway). See [homelab naming convention](https://github.com/duynhlab/homelab/blob/main/docs/api/api-naming-convention.md).

| Method | Cluster path | Edge path (via gateway) |
|--------|--------------|-------------------------|
| `GET` | `/api/v1/users/:id` | `/user/v1/public/users/:id` |
| `GET` | `/api/v1/users/profile` | `/user/v1/private/users/profile` |
| `PUT` | `/api/v1/users/profile` | `/user/v1/private/users/profile` |
| `POST` | `/api/v1/users` | *(internal — not on gateway)* |

## Tech Stack

- Go + Gin framework
- PostgreSQL 16 (supporting-db cluster)
- PgBouncer connection pooling
- OpenTelemetry tracing

## Development

### Prerequisites

- Go 1.26.0+
- [golangci-lint](https://golangci-lint.run/welcome/install/) v2+

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

### Pre-push Checklist

```bash
go build ./... && go test ./... && golangci-lint run --timeout=10m
```

## License

MIT

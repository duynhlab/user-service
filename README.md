# user-service

User management microservice for profiles and account operations.

## Features

- User profile management
- Account operations
- User search

## API Endpoints

All routes follow Variant A naming — single path for browser and in-cluster callers. See [homelab naming convention](https://github.com/duynhlab/homelab/blob/main/docs/api/api-naming-convention.md).

| Method | Path | Audience |
|--------|------|----------|
| `GET` | `/user/v1/public/users/:id` | public |
| `GET` | `/user/v1/private/users/profile` | private |
| `PUT` | `/user/v1/private/users/profile` | private |
| `POST` | `/user/v1/internal/users` | internal (auth-service during registration; in-cluster only) |

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

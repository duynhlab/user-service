# user-service

User management microservice for profiles and account operations.

## Features

- User profile management
- Account operations
- User search

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/users/:id` | Get user by ID |
| `GET` | `/api/v1/users/profile` | Get user profile |
| `PUT` | `/api/v1/users/profile` | Update profile |

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

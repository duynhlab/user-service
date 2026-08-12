//go:build integration

// Integration tests for the PostgreSQL UserRepository. They run a real Postgres
// via testcontainers-go and apply the service's schema migrations plus the
// dev-only demo seed, so they exercise the actual SQL (not a mock). Run with:
//
//	go test -tags=integration ./internal/core/repository/...
//
// Requires a reachable Docker daemon. Excluded from the default `go test ./...`
// unit run by the `integration` build tag.
package psql

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/duynhlab/user-service/internal/core/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("user"),
		postgres.WithUsername("user"),
		postgres.WithPassword("secret"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	applyMigrations(t, ctx, dsn)
	applySeed(t, ctx, dsn)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// applyMigrations runs every db/migrations/sql/*.up.sql in lexical order.
func applyMigrations(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	applySQLDir(t, ctx, dsn, filepath.Join("..", "..", "..", "..", "db", "migrations", "sql"))
}

// applySeed applies the dev-only demo seed (db/seed/sql) — it lives outside the
// migration chain, so read-path tests must load it explicitly here.
func applySeed(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	applySQLDir(t, ctx, dsn, filepath.Join("..", "..", "..", "..", "db", "seed", "sql"))
}

// applySQLDir runs every *.up.sql in dir in lexical order using a simple-protocol
// connection (so multi-statement files execute in one round).
func applySQLDir(t *testing.T, ctx context.Context, dsn, dir string) {
	t.Helper()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect for %s: %v", dir, err)
	}
	defer conn.Close(ctx)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, f := range files {
		sqlBytes, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if _, err := conn.Exec(ctx, string(sqlBytes)); err != nil {
			t.Fatalf("apply %s: %v", f, err)
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Fixed realm subjects of the Keycloak demo users (ADR-041) — the seed keys
// user_profiles.user_id by these opaque OIDC subject strings.
const (
	aliceSub   = "a11ce000-0000-4000-8000-000000000001"
	missingSub = "00000000-0000-4000-8000-000000000999"
)

func TestUserRepository_Integration(t *testing.T) {
	pool := newTestDB(t)
	repo := NewUserRepository(pool)
	ctx := context.Background()

	t.Run("GetProfileByUserID returns seeded profile", func(t *testing.T) {
		p, err := repo.GetProfileByUserID(ctx, aliceSub) // Alice Johnson (seed)
		if err != nil {
			t.Fatalf("GetProfileByUserID(alice): %v", err)
		}
		if deref(p.FirstName) != "Alice" || deref(p.LastName) != "Johnson" {
			t.Errorf("profile = %s %s, want Alice Johnson", deref(p.FirstName), deref(p.LastName))
		}
		if p.UserID != aliceSub {
			t.Errorf("UserID = %q, want %q", p.UserID, aliceSub)
		}
	})

	t.Run("GetProfileByUserID missing -> (nil, nil), service layer maps to not-found", func(t *testing.T) {
		p, err := repo.GetProfileByUserID(ctx, missingSub)
		if err != nil || p != nil {
			t.Errorf("GetProfileByUserID(missing) = (%v, %v), want (nil, nil)", p, err)
		}
	})

	t.Run("GetUser by subject string", func(t *testing.T) {
		if _, err := repo.GetUser(ctx, aliceSub); err != nil {
			t.Errorf("GetUser(alice): %v", err)
		}
		if _, err := repo.GetUser(ctx, missingSub); !errors.Is(err, domain.ErrUserNotFound) {
			t.Errorf("GetUser(missing) err = %v, want ErrUserNotFound", err)
		}
		if _, err := repo.GetUser(ctx, ""); !errors.Is(err, domain.ErrUserNotFound) {
			t.Errorf("GetUser(empty subject) err = %v, want ErrUserNotFound", err)
		}
	})

	t.Run("Update missing row reports not found", func(t *testing.T) {
		updated, err := repo.UpdateUserProfile(ctx, missingSub, "New", "Name", "+1-555-9999")
		if err != nil || updated {
			t.Errorf("UpdateUserProfile(missing) = (%v,%v), want (false,nil)", updated, err)
		}
	})

	t.Run("Upsert (JIT) creates then Get then Update", func(t *testing.T) {
		const uid = "11111111-1111-4111-8111-000000000100"
		if err := repo.UpsertUserProfile(ctx, uid, "Test", "User", ""); err != nil {
			t.Fatalf("UpsertUserProfile (insert): %v", err)
		}
		updated, err := repo.UpdateUserProfile(ctx, uid, "New", "Name", "+1-555-9999")
		if err != nil || !updated {
			t.Fatalf("UpdateUserProfile = (%v,%v), want (true,nil)", updated, err)
		}
		p, err := repo.GetProfileByUserID(ctx, uid)
		if err != nil {
			t.Fatalf("GetProfileByUserID(%s): %v", uid, err)
		}
		if deref(p.FirstName) != "New" || deref(p.LastName) != "Name" || deref(p.Phone) != "+1-555-9999" {
			t.Errorf("after update = %+v, want New Name / +1-555-9999", p)
		}
	})

	t.Run("UpsertUserProfile creates then updates", func(t *testing.T) {
		const uid = "11111111-1111-4111-8111-000000000101"
		if err := repo.UpsertUserProfile(ctx, uid, "Up", "Sert", "+1-555-0000"); err != nil {
			t.Fatalf("UpsertUserProfile (insert): %v", err)
		}
		if err := repo.UpsertUserProfile(ctx, uid, "Up2", "Sert2", "+1-555-1111"); err != nil {
			t.Fatalf("UpsertUserProfile (update): %v", err)
		}
		p, err := repo.GetProfileByUserID(ctx, uid)
		if err != nil {
			t.Fatalf("GetProfileByUserID(%s): %v", uid, err)
		}
		if deref(p.FirstName) != "Up2" {
			t.Errorf("after upsert-update FirstName = %q, want Up2", deref(p.FirstName))
		}
	})
}

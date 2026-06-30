//go:build integration

// Integration tests for the PostgreSQL UserRepository. They run a real Postgres
// via testcontainers-go and apply the service's migrations (schema + seed), so
// they exercise the actual SQL (not a mock). Run with:
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

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// applyMigrations runs every db/migrations/sql/*.up.sql in lexical order using a
// simple-protocol connection (so multi-statement files execute in one round).
func applyMigrations(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect for migrations: %v", err)
	}
	defer conn.Close(ctx)

	dir := filepath.Join("..", "..", "..", "..", "db", "migrations", "sql")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
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
			t.Fatalf("apply migration %s: %v", f, err)
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TestUserRepository_Integration(t *testing.T) {
	pool := newTestDB(t)
	repo := NewUserRepository(pool)
	ctx := context.Background()

	t.Run("GetProfileByUserID returns seeded profile", func(t *testing.T) {
		p, err := repo.GetProfileByUserID(ctx, 1) // Alice Johnson (seed)
		if err != nil {
			t.Fatalf("GetProfileByUserID(1): %v", err)
		}
		if deref(p.FirstName) != "Alice" || deref(p.LastName) != "Johnson" {
			t.Errorf("profile = %s %s, want Alice Johnson", deref(p.FirstName), deref(p.LastName))
		}
	})

	t.Run("GetProfileByUserID missing -> (nil, nil), service layer maps to not-found", func(t *testing.T) {
		p, err := repo.GetProfileByUserID(ctx, 999999)
		if err != nil || p != nil {
			t.Errorf("GetProfileByUserID(missing) = (%v, %v), want (nil, nil)", p, err)
		}
	})

	t.Run("GetUser by id string", func(t *testing.T) {
		if _, err := repo.GetUser(ctx, "1"); err != nil {
			t.Errorf("GetUser(1): %v", err)
		}
		if _, err := repo.GetUser(ctx, "999999"); !errors.Is(err, domain.ErrUserNotFound) {
			t.Errorf("GetUser(missing) err = %v, want ErrUserNotFound", err)
		}
		if _, err := repo.GetUser(ctx, "not-a-number"); !errors.Is(err, domain.ErrUserNotFound) {
			t.Errorf("GetUser(non-numeric) err = %v, want ErrUserNotFound", err)
		}
	})

	t.Run("CheckProfileExists", func(t *testing.T) {
		if ok, err := repo.CheckProfileExists(ctx, 1); err != nil || !ok {
			t.Errorf("CheckProfileExists(1) = (%v,%v), want (true,nil)", ok, err)
		}
		if ok, err := repo.CheckProfileExists(ctx, 999999); err != nil || ok {
			t.Errorf("CheckProfileExists(missing) = (%v,%v), want (false,nil)", ok, err)
		}
	})

	t.Run("Create then Get then Update", func(t *testing.T) {
		const uid = 100
		id, err := repo.CreateUserProfile(ctx, uid, "Test", "User")
		if err != nil {
			t.Fatalf("CreateUserProfile: %v", err)
		}
		if id <= 0 {
			t.Errorf("CreateUserProfile id = %d, want > 0", id)
		}
		updated, err := repo.UpdateUserProfile(ctx, uid, "New", "Name", "+1-555-9999")
		if err != nil || !updated {
			t.Fatalf("UpdateUserProfile = (%v,%v), want (true,nil)", updated, err)
		}
		p, err := repo.GetProfileByUserID(ctx, uid)
		if err != nil {
			t.Fatalf("GetProfileByUserID(%d): %v", uid, err)
		}
		if deref(p.FirstName) != "New" || deref(p.LastName) != "Name" || deref(p.Phone) != "+1-555-9999" {
			t.Errorf("after update = %+v, want New Name / +1-555-9999", p)
		}
	})

	t.Run("UpsertUserProfile creates then updates", func(t *testing.T) {
		const uid = 101
		if err := repo.UpsertUserProfile(ctx, uid, "Up", "Sert", "+1-555-0000"); err != nil {
			t.Fatalf("UpsertUserProfile (insert): %v", err)
		}
		if err := repo.UpsertUserProfile(ctx, uid, "Up2", "Sert2", "+1-555-1111"); err != nil {
			t.Fatalf("UpsertUserProfile (update): %v", err)
		}
		p, err := repo.GetProfileByUserID(ctx, uid)
		if err != nil {
			t.Fatalf("GetProfileByUserID(%d): %v", uid, err)
		}
		if deref(p.FirstName) != "Up2" {
			t.Errorf("after upsert-update FirstName = %q, want Up2", deref(p.FirstName))
		}
	})
}

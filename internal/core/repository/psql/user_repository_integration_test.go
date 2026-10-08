//go:build integration

// Integration tests for the PostgreSQL UserRepository. They run a real Postgres
// via testcontainers-go and apply the service's schema migrations plus the
// dev-only demo seed as the migrator, then run as the runtime login, so they
// exercise the actual SQL and grants (not a mock). Run with:
//
//	go test -tags=integration ./internal/core/repository/...
//
// Requires a reachable Docker daemon. Excluded from the default `go test ./...`
// unit run by the `integration` build tag.
package psql

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/duynhlab/pkg/migratex"
	"github.com/duynhlab/user-service/db/migrations"
	"github.com/duynhlab/user-service/db/seed"
	"github.com/duynhlab/user-service/internal/core/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// testDB holds the three logins of the RFC-0029 shape: a superuser that plays
// the platform (creates the roles, as CNPG does), the migrator, and the
// runtime pool the repository runs on.
type testDB struct {
	adminDSN    string
	migratorDSN string
	runtimeDSN  string
	runtime     *pgxpool.Pool
}

// startWithRoles starts a throwaway Postgres and creates user_owner /
// user_migrator / user_runtime the way the platform does. Everything is
// torn down via t.Cleanup.
func startWithRoles(t *testing.T) *testDB {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:18-alpine",
		postgres.WithDatabase("user"),
		postgres.WithUsername("platform"),
		postgres.WithPassword("secret"),
		// Ready twice (initdb restarts the server once), then the published
		// port: the module's own strategy, so a test never races the restart.
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	adminDSN, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	db := &testDB{
		adminDSN:    adminDSN,
		migratorDSN: withUser(t, adminDSN, "user_migrator", "migrator"),
		runtimeDSN:  withUser(t, adminDSN, "user_runtime", "runtime"),
	}

	execAll(t, ctx, adminDSN,
		`CREATE ROLE user_owner NOLOGIN`,
		`CREATE ROLE user_migrator LOGIN NOINHERIT PASSWORD 'migrator'`,
		`CREATE ROLE user_runtime LOGIN PASSWORD 'runtime'`,
		`GRANT user_owner TO user_migrator WITH INHERIT FALSE, SET TRUE, ADMIN FALSE`,
		// The platform makes the owner own the database; on PG15+ that is what
		// gives it CREATE on the public schema (owned by pg_database_owner).
		// "user" is a reserved word, so the database name is quoted.
		`ALTER DATABASE "user" OWNER TO user_owner`,
	)
	return db
}

// newBareDB is newTestDB without migrations or seed; it returns the
// migrator's DSN.
func newBareDB(t *testing.T) string {
	t.Helper()
	return startWithRoles(t).migratorDSN
}

// newTestDB starts a throwaway Postgres with the three roles, migrates and
// seeds as the migrator after SET ROLE user_owner, and returns it with a
// pool connected as user_runtime.
func newTestDB(t *testing.T) *testDB {
	t.Helper()
	ctx := context.Background()
	db := startWithRoles(t)

	if err := migratex.Run(migrations.FS, "sql", db.migratorDSN, migratex.WithSetRole(ownerRole)); err != nil {
		t.Fatalf("migrate as the migrator: %v", err)
	}
	if err := seed.Apply(ctx, db.migratorDSN, ownerRole); err != nil {
		t.Fatalf("seed as the migrator: %v", err)
	}

	pool, err := pgxpool.New(ctx, db.runtimeDSN)
	if err != nil {
		t.Fatalf("new runtime pool: %v", err)
	}
	t.Cleanup(pool.Close)
	db.runtime = pool
	return db
}

const ownerRole = "user_owner"

func withUser(t *testing.T, dsn, user, password string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.User = url.UserPassword(user, password)
	return u.String()
}

func execAll(t *testing.T, ctx context.Context, dsn string, stmts ...string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	for _, stmt := range stmts {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
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
	db := newTestDB(t)
	repo := NewUserRepository(db.runtime)
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

// The RFC-0029 authorization contract, checked as the real logins: the owner
// owns every object, the runtime can serve traffic and nothing more, and the
// migration refuses to run without its role.
func TestAuthorization_Integration(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	t.Run("every relation belongs to user_owner", func(t *testing.T) {
		// Extension members are owned by whoever created the extension, not
		// by the schema owner, so they are left out.
		rows, err := db.runtime.Query(ctx, `
			SELECT c.relname || ':' || pg_get_userbyid(c.relowner)
			  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname = 'public' AND pg_get_userbyid(c.relowner) <> 'user_owner'
			   AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid AND d.deptype = 'e')`)
		if err != nil {
			t.Fatalf("query owners: %v", err)
		}
		others, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatalf("scan owners: %v", err)
		}
		if len(others) != 0 {
			t.Fatalf("relations not owned by user_owner: %v", others)
		}
	})

	t.Run("the runtime cannot change the schema or reach the migration table", func(t *testing.T) {
		for _, stmt := range []string{
			`CREATE TABLE public.evil (i int)`,
			`ALTER TABLE public.user_profiles ADD COLUMN evil int`,
			`DROP TABLE public.user_profiles`,
			`SELECT version FROM public.schema_migrations`,
			`SET ROLE user_owner`,
		} {
			_, err := db.runtime.Exec(ctx, stmt)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || (pgErr.Code != "42501" && pgErr.Code != "42704") {
				t.Errorf("%s as user_runtime: got %v, want permission denied", stmt, err)
			}
		}
		// Without GRANT OPTION, PostgreSQL only warns "no privileges were
		// granted"; the assertion is the effect, not an error code.
		if _, err := db.runtime.Exec(ctx, `GRANT SELECT ON public.user_profiles TO PUBLIC`); err != nil {
			t.Fatalf("grant attempt: %v", err)
		}
		var leaked bool
		if err := db.runtime.QueryRow(ctx,
			`SELECT has_table_privilege('public', 'public.user_profiles', 'SELECT')`).Scan(&leaked); err != nil {
			t.Fatalf("check PUBLIC access: %v", err)
		}
		if leaked {
			t.Fatal("user_runtime handed SELECT on user_profiles to PUBLIC")
		}
	})

	t.Run("migrate and seed refuse an empty DB_MIGRATION_ROLE", func(t *testing.T) {
		if err := migratex.Run(migrations.FS, "sql", db.migratorDSN, migratex.WithSetRole("")); err == nil {
			t.Fatal("migrate with an empty role succeeded")
		}
		if err := seed.Apply(ctx, db.migratorDSN, ""); err == nil {
			t.Fatal("seed with an empty role succeeded")
		}
	})

	t.Run("the migrator creates nothing as itself", func(t *testing.T) {
		// No SET ROLE at all: the NOINHERIT migrator has no right on the
		// owner's schema, so even golang-migrate's version table is refused.
		fresh := newBareDB(t)
		err := migratex.Run(migrations.FS, "sql", fresh)
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("migrate without SET ROLE = %v, want permission denied", err)
		}
	})

	t.Run("seed as a role the login cannot switch to fails", func(t *testing.T) {
		err := seed.Apply(ctx, db.runtimeDSN, ownerRole)
		if err == nil || !strings.Contains(err.Error(), "SET ROLE") {
			t.Fatalf("seed as user_runtime = %v, want SET ROLE error", err)
		}
	})
}

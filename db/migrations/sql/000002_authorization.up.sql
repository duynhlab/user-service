-- Authorization for the runtime login (homelab RFC-0029).
--
-- Runs as user_owner: the migrate subcommand logs in as user_migrator and
-- switches with SET ROLE, so every object below and every later one belongs to
-- the owner. user_runtime gets CRUD only; it never owns, alters or drops.
--
-- The roles are created by the platform before migrations run (CNPG on the
-- cluster, init.sql in local-stack, the test setup in CI). A missing role
-- fails this migration on purpose.

GRANT USAGE ON SCHEMA public TO user_runtime;

-- Objects that already exist. Granted by name, not ON ALL TABLES, so the
-- runtime never reaches schema_migrations.
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE public.user_profiles TO user_runtime;
GRANT USAGE, SELECT ON SEQUENCE public.user_profiles_id_seq TO user_runtime;

-- Every table and sequence a later migration creates.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO user_runtime;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO user_runtime;

-- Global, not IN SCHEMA: a per-schema revoke cannot cancel the built-in
-- PUBLIC EXECUTE on functions (RFC-0029 PG-05).
ALTER DEFAULT PRIVILEGES REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;

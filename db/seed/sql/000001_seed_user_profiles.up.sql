-- =============================================================================
-- User Service - Demo Seed Data (DEV ONLY)
-- =============================================================================
-- Purpose: Demo user profiles for local/dev/demo environments only.
-- Applied ONLY by the `seed` subcommand — NEVER by `migrate` or the serve path,
-- so production databases are never seeded with these profiles.
-- Note: user_id is the OIDC token subject — the fixed ids of the Keycloak
-- realm demo users (alice..eve, a11ce000-0000-4000-8000-00000000000N; ADR-041).
-- =============================================================================

INSERT INTO user_profiles (id, user_id, first_name, last_name, phone, address, created_at, updated_at) VALUES
    (1, 'a11ce000-0000-4000-8000-000000000001', 'Alice', 'Johnson', '+1-555-0101', '123 Main St, San Francisco, CA 94102', NOW() - INTERVAL '30 days', NOW() - INTERVAL '5 days'),
    (2, 'a11ce000-0000-4000-8000-000000000002', 'Bob', 'Smith', '+1-555-0102', '456 Oak Ave, Seattle, WA 98101', NOW() - INTERVAL '25 days', NOW() - INTERVAL '10 days'),
    (3, 'a11ce000-0000-4000-8000-000000000003', 'Carol', 'White', '+1-555-0103', '789 Pine Rd, Portland, OR 97201', NOW() - INTERVAL '20 days', NOW() - INTERVAL '2 days'),
    (4, 'a11ce000-0000-4000-8000-000000000004', 'David', 'Brown', '+1-555-0104', '321 Elm St, Austin, TX 78701', NOW() - INTERVAL '15 days', NOW() - INTERVAL '1 day'),
    (5, 'a11ce000-0000-4000-8000-000000000005', 'Eve', 'Davis', '+1-555-0105', '654 Maple Dr, Boston, MA 02101', NOW() - INTERVAL '60 days', NOW() - INTERVAL '60 days')
ON CONFLICT (user_id) DO NOTHING;

-- Realign the sequence to MAX(id): the seed rows use explicit ids, so without
-- this the first app INSERT collides on the primary key.
SELECT setval('user_profiles_id_seq', (SELECT MAX(id) FROM user_profiles));

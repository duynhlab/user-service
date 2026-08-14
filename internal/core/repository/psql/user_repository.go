package psql

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/duynhlab/user-service/internal/core/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserRepository implements domain.UserRepository using PostgreSQL
type UserRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository creates a new PostgreSQL user repository
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// GetUser retrieves the public view of a user by ID.
//
// The id is the OIDC token subject — an opaque string (ADR-041). user-service
// owns only the user_profiles table; the authoritative identity lives in the
// Keycloak realm. We therefore resolve the public Name from user_profiles and
// leave Username/Email empty — the public endpoint omits them anyway. A
// missing profile row maps to domain.ErrUserNotFound.
func (r *UserRepository) GetUser(ctx context.Context, id string) (*domain.User, error) {
	if r.pool == nil {
		return nil, errors.New("database connection not available")
	}

	if id == "" {
		return nil, fmt.Errorf("empty user id: %w", domain.ErrUserNotFound)
	}

	profile, err := r.GetProfileByUserID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user %q: %w", id, err)
	}
	if profile == nil {
		return nil, domain.ErrUserNotFound
	}

	name := profileName(profile)
	if name == "" {
		name = "User " + id
	}

	return &domain.User{
		ID:   id,
		Name: name,
	}, nil
}

// profileName joins the profile's first and last name, skipping empty parts.
func profileName(profile *domain.UserProfile) string {
	parts := make([]string, 0, 2)
	if profile.FirstName != nil && *profile.FirstName != "" {
		parts = append(parts, *profile.FirstName)
	}
	if profile.LastName != nil && *profile.LastName != "" {
		parts = append(parts, *profile.LastName)
	}
	return strings.Join(parts, " ")
}

// GetProfileByUserID retrieves a user profile by user ID (OIDC subject string)
func (r *UserRepository) GetProfileByUserID(ctx context.Context, userID string) (*domain.UserProfile, error) {
	db := r.pool
	if db == nil {
		return nil, errors.New("database connection not available")
	}

	var profile domain.UserProfile
	query := `SELECT id, user_id, first_name, last_name, phone, address, created_at, updated_at FROM user_profiles WHERE user_id = $1`

	err := db.QueryRow(ctx, query, userID).Scan(
		&profile.ID,
		&profile.UserID,
		&profile.FirstName,
		&profile.LastName,
		&profile.Phone,
		&profile.Address,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // Return nil if not found, let service handle it
		}
		return nil, fmt.Errorf("query user profile: %w", err)
	}

	return &profile, nil
}

// UpdateUserProfile updates an existing user profile
// Returns true if updated, false if not found
func (r *UserRepository) UpdateUserProfile(ctx context.Context, userID string, firstName, lastName, phone string) (bool, error) {
	db := r.pool
	if db == nil {
		return false, errors.New("database connection not available")
	}

	query := `UPDATE user_profiles SET first_name = COALESCE(NULLIF($1, ''), first_name), last_name = COALESCE(NULLIF($2, ''), last_name), phone = COALESCE(NULLIF($3, ''), phone) WHERE user_id = $4`
	result, err := db.Exec(ctx, query, firstName, lastName, phone, userID)
	if err != nil {
		return false, fmt.Errorf("update profile: %w", err)
	}

	return result.RowsAffected() > 0, nil
}

// UpsertUserProfile creates or updates a user profile. This is the JIT
// provisioning write path: the first PUT from a verified token creates the row.
func (r *UserRepository) UpsertUserProfile(ctx context.Context, userID string, firstName, lastName, phone string) error {
	// Try update first
	updated, err := r.UpdateUserProfile(ctx, userID, firstName, lastName, phone)
	if err != nil {
		return err
	}
	if updated {
		return nil
	}

	// If not updated, create
	db := r.pool
	query := `INSERT INTO user_profiles (user_id, first_name, last_name, phone) VALUES ($1, $2, $3, $4)`
	_, err = db.Exec(ctx, query, userID, firstName, lastName, phone)
	if err != nil {
		return fmt.Errorf("create profile: %w", err)
	}
	return nil
}

// SearchProfiles implements the Backoffice operator search (RFC-0023):
// case-insensitive name/phone match or exact user_id, newest first.
func (r *UserRepository) SearchProfiles(ctx context.Context, query string, limit, offset int) ([]domain.UserProfile, int, error) {
	where := ""
	args := []any{}
	if query != "" {
		args = append(args, "%"+query+"%", query)
		where = ` WHERE first_name ILIKE $1 OR last_name ILIKE $1 OR phone ILIKE $1 OR user_id = $2`
	}

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM user_profiles"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count profiles: %w", err)
	}

	q := fmt.Sprintf(`
		SELECT id, user_id, first_name, last_name, phone, address, created_at, updated_at
		FROM user_profiles%s
		ORDER BY id DESC
		LIMIT $%d OFFSET $%d`, where, len(args)+1, len(args)+2)
	rows, err := r.pool.Query(ctx, q, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("search profiles: %w", err)
	}
	defer rows.Close()

	items := make([]domain.UserProfile, 0)
	for rows.Next() {
		var p domain.UserProfile
		if err := rows.Scan(&p.ID, &p.UserID, &p.FirstName, &p.LastName, &p.Phone, &p.Address, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan profile: %w", err)
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate profiles: %w", err)
	}
	return items, total, nil
}

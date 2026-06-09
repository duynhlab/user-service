package psql

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
// user-service owns only the user_profiles table; the authoritative users
// table (with username/email) lives in auth-service across a cluster boundary
// with no FK. We therefore resolve the public Name from user_profiles and
// leave Username/Email empty — the public endpoint omits them anyway. A
// missing profile row maps to domain.ErrUserNotFound.
func (r *UserRepository) GetUser(ctx context.Context, id string) (*domain.User, error) {
	if r.pool == nil {
		return nil, errors.New("database connection not available")
	}

	userID, err := strconv.Atoi(id)
	if err != nil {
		return nil, fmt.Errorf("parse user id %q: %w", id, domain.ErrUserNotFound)
	}

	profile, err := r.GetProfileByUserID(ctx, userID)
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

// GetProfileByUserID retrieves a user profile by user ID
func (r *UserRepository) GetProfileByUserID(ctx context.Context, userID int) (*domain.UserProfile, error) {
	db := r.pool
	if db == nil {
		return nil, errors.New("database connection not available")
	}

	var profile domain.UserProfile
	query := `SELECT id, user_id, first_name, last_name, phone, address FROM user_profiles WHERE user_id = $1`

	err := db.QueryRow(ctx, query, userID).Scan(
		&profile.ID,
		&profile.UserID,
		&profile.FirstName,
		&profile.LastName,
		&profile.Phone,
		&profile.Address,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // Return nil if not found, let service handle it
		}
		return nil, fmt.Errorf("query user profile: %w", err)
	}

	return &profile, nil
}

// CreateUserProfile creates a new user profile
func (r *UserRepository) CreateUserProfile(ctx context.Context, userID int, firstName, lastName string) (int, error) {
	db := r.pool
	if db == nil {
		return 0, errors.New("database connection not available")
	}

	query := `INSERT INTO user_profiles (user_id, first_name, last_name) VALUES ($1, $2, $3) RETURNING id`
	var profileID int
	err := db.QueryRow(ctx, query, userID, firstName, lastName).Scan(&profileID)
	if err != nil {
		return 0, fmt.Errorf("insert user profile: %w", err)
	}
	return profileID, nil
}

// UpdateUserProfile updates an existing user profile
// Returns true if updated, false if not found
func (r *UserRepository) UpdateUserProfile(ctx context.Context, userID int, firstName, lastName, phone string) (bool, error) {
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

// CheckProfileExists checks if a profile exists for a user ID
func (r *UserRepository) CheckProfileExists(ctx context.Context, userID int) (bool, error) {
	db := r.pool
	if db == nil {
		return false, errors.New("database connection not available")
	}

	var id int
	query := `SELECT id FROM user_profiles WHERE user_id = $1`
	err := db.QueryRow(ctx, query, userID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("check profile exists: %w", err)
	}
	return true, nil
}

// UpsertUserProfile creates or updates a user profile
func (r *UserRepository) UpsertUserProfile(ctx context.Context, userID int, firstName, lastName, phone string) error {
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

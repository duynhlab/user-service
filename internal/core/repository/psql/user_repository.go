package psql

import (
	"context"
	"errors"
	"fmt"

	database "github.com/duynhlab/user-service/internal/core"
	"github.com/duynhlab/user-service/internal/core/domain"
	"github.com/jackc/pgx/v5"
)

// UserRepository implements domain.UserRepository using PostgreSQL
type UserRepository struct{}

// NewUserRepository creates a new PostgreSQL user repository
func NewUserRepository() *UserRepository {
	return &UserRepository{}
}

// GetUser retrieves a user by ID
// Note: This matches the previous mock behavior in logic layer.
// Since user-service doesn't own the 'users' table (auth-service does),
// we can't reliably get username/email from DB here without calling Auth Service.
// For now, we keep the mock behavior but move it to repository as the "Data Source".
func (r *UserRepository) GetUser(ctx context.Context, id string) (*domain.User, error) {
	// Mock logic preserved from original service
	if id == "999" {
		return nil, domain.ErrUserNotFound // We need to make sure this error is available or use a standard error
	}

	return &domain.User{
		ID:       id,
		Username: "user" + id,
		Email:    "user" + id + "@example.com",
		Name:     "User " + id,
	}, nil
}

// GetProfileByUserID retrieves a user profile by user ID
func (r *UserRepository) GetProfileByUserID(ctx context.Context, userID int) (*domain.UserProfile, error) {
	db := database.GetPool()
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
	db := database.GetPool()
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
	db := database.GetPool()
	if db == nil {
		return false, errors.New("database connection not available")
	}

	query := `UPDATE user_profiles SET first_name = $1, last_name = $2, phone = $3 WHERE user_id = $4`
	result, err := db.Exec(ctx, query, firstName, lastName, phone, userID)
	if err != nil {
		return false, fmt.Errorf("update profile: %w", err)
	}

	return result.RowsAffected() > 0, nil
}

// CheckProfileExists checks if a profile exists for a user ID
func (r *UserRepository) CheckProfileExists(ctx context.Context, userID int) (bool, error) {
	db := database.GetPool()
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
	db := database.GetPool()
	query := `INSERT INTO user_profiles (user_id, first_name, last_name, phone) VALUES ($1, $2, $3, $4)`
	_, err = db.Exec(ctx, query, userID, firstName, lastName, phone)
	if err != nil {
		return fmt.Errorf("create profile: %w", err)
	}
	return nil
}

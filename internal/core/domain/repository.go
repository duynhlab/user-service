package domain

import "context"

// UserRepository defines the interface for user data access.
// User ids are OIDC token subjects — opaque strings (ADR-041).
type UserRepository interface {
	GetUser(ctx context.Context, id string) (*User, error)
	GetProfileByUserID(ctx context.Context, userID string) (*UserProfile, error)
	UpdateUserProfile(ctx context.Context, userID string, firstName, lastName, phone string) (bool, error)
	UpsertUserProfile(ctx context.Context, userID string, firstName, lastName, phone string) error
}

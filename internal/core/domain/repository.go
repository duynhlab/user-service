package domain

import "context"

// UserRepository defines the interface for user data access.
// User ids are OIDC token subjects — opaque strings (ADR-041).
type UserRepository interface {
	GetUser(ctx context.Context, id string) (*User, error)
	GetProfileByUserID(ctx context.Context, userID string) (*UserProfile, error)
	UpdateUserProfile(ctx context.Context, userID string, firstName, lastName, phone string) (bool, error)
	UpsertUserProfile(ctx context.Context, userID string, firstName, lastName, phone string) error

	// SearchProfiles serves the Backoffice's operator search (RFC-0023 slice
	// A): a case-insensitive match on first/last name, phone, or the exact
	// user_id, one page (newest first) plus the unpaged total. Identity
	// claims (email/username) live in Keycloak, not here — name/phone/sub is
	// the whole searchable surface. Seq scan is acceptable at homelab scale.
	SearchProfiles(ctx context.Context, query string, limit, offset int) ([]UserProfile, int, error)
}

package v1

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/duynhlab/user-service/internal/core/domain"
	"github.com/duynhlab/user-service/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// UserService defines the business logic for user management
type UserService struct {
	repo domain.UserRepository
}

// NewUserService creates a new user service with injected repository
func NewUserService(repo domain.UserRepository) *UserService {
	return &UserService{
		repo: repo,
	}
}

// GetUser retrieves a user by ID
func (s *UserService) GetUser(ctx context.Context, id string) (*domain.User, error) {
	_, span := middleware.StartSpan(ctx, "user.get", trace.WithAttributes(
		attribute.String("layer", "logic"),
		attribute.String("user.id", id),
	))
	defer span.End()

	user, err := s.repo.GetUser(ctx, id)
	if err != nil {
		span.SetAttributes(attribute.Bool("user.found", false))
		// A missing profile is a bounded "miss"; other errors are internal
		// failures counted via the DB span, not this business counter.
		if errors.Is(err, domain.ErrUserNotFound) {
			recordProfileLookup(ctx, audiencePublic, false)
		}
		return nil, fmt.Errorf("get user by id %q: %w", id, err)
	}

	span.SetAttributes(attribute.Bool("user.found", true))
	recordProfileLookup(ctx, audiencePublic, true)
	return user, nil
}

// GetProfile retrieves the current user's profile.
// userID is the verified OIDC token subject (opaque string); username and
// email are the verified token claims — all set by the auth middleware.
func (s *UserService) GetProfile(ctx context.Context, userID string, username, email string) (*domain.User, error) {
	ctx, span := middleware.StartSpan(ctx, "user.profile", trace.WithAttributes(
		attribute.String("layer", "logic"),
		attribute.String("user.id", userID),
	))
	defer span.End()

	// An empty subject cannot be attributed to a principal; authmw rejects
	// such tokens, so this is defence-in-depth.
	if userID == "" {
		span.SetAttributes(attribute.Bool("profile.found", false))
		return nil, fmt.Errorf("empty user_id: %w", domain.ErrUserNotFound)
	}

	// Fetch profile from repository
	profile, err := s.repo.GetProfileByUserID(ctx, userID)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("query user profile: %w", err)
	}

	// JIT fallback: no profile row yet — build the identity from the verified
	// token claims only. The first PUT upsert creates the row.
	if profile == nil {
		span.SetAttributes(attribute.Bool("profile.found", false))
		recordProfileLookup(ctx, audiencePrivate, false)
		return &domain.User{
			ID:       userID,
			Username: username,
			Email:    email,
			Name:     "User " + userID,
		}, nil
	}

	// Build name from profile
	nameParts := []string{}
	if profile.FirstName != nil && *profile.FirstName != "" {
		nameParts = append(nameParts, *profile.FirstName)
	}
	if profile.LastName != nil && *profile.LastName != "" {
		nameParts = append(nameParts, *profile.LastName)
	}
	name := strings.Join(nameParts, " ")
	if name == "" {
		name = "User " + userID
	}

	// Build phone string
	phoneStr := ""
	if profile.Phone != nil && *profile.Phone != "" {
		phoneStr = *profile.Phone
	}

	user := &domain.User{
		ID:       userID,
		Username: username,
		Email:    email,
		Name:     name,
		Phone:    phoneStr,
	}

	span.SetAttributes(attribute.Bool("profile.found", true))
	recordProfileLookup(ctx, audiencePrivate, true)
	return user, nil
}

// UpdateProfile upserts the current user's profile. userID is the verified
// OIDC token subject; the upsert is the JIT provisioning write path.
func (s *UserService) UpdateProfile(ctx context.Context, userID string, req domain.UpdateProfileRequest) (*domain.User, error) {
	ctx, span := middleware.StartSpan(ctx, "user.update_profile", trace.WithAttributes(
		attribute.String("layer", "logic"),
		attribute.String("user_id", userID),
	))
	defer span.End()

	// An empty subject cannot be attributed to a principal; authmw rejects
	// such tokens, so this is defence-in-depth.
	if userID == "" {
		span.SetAttributes(attribute.Bool("profile.updated", false))
		recordProfileUpdated(ctx, resultUnauthorized)
		return nil, fmt.Errorf("empty user_id: %w", domain.ErrUnauthorized)
	}

	// Parse name
	nameParts := strings.Fields(req.Name)
	var firstName, lastName string
	if len(nameParts) > 0 {
		firstName = nameParts[0]
	}
	if len(nameParts) > 1 {
		lastName = strings.Join(nameParts[1:], " ")
	}

	// Upsert profile
	err := s.repo.UpsertUserProfile(ctx, userID, firstName, lastName, req.Phone)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("upsert profile: %w", err)
	}

	user := &domain.User{
		ID:   userID,
		Name: req.Name,
	}

	span.SetAttributes(attribute.Bool("profile.updated", true))
	recordProfileUpdated(ctx, resultSuccess)
	return user, nil
}

// SearchProfiles serves the Backoffice operator search (RFC-0023 slice A) —
// a pass-through with the platform's observability conventions; the sensitive
// shaping (what an operator may see) happens in the web layer.
func (s *UserService) SearchProfiles(ctx context.Context, query string, limit, offset int) ([]domain.UserProfile, int, error) {
	items, total, err := s.repo.SearchProfiles(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("search profiles: %w", err)
	}
	return items, total, nil
}

// GetProfileRecord returns the raw profile row for the Backoffice case view
// (RFC-0023). Unlike GetProfile it does not synthesize display fields from
// token claims — the operator sees exactly what this service stores.
func (s *UserService) GetProfileRecord(ctx context.Context, userID string) (*domain.UserProfile, error) {
	profile, err := s.repo.GetProfileByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get profile record: %w", err)
	}
	if profile == nil {
		return nil, domain.ErrUserNotFound
	}
	return profile, nil
}

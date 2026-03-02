package v1

import (
	"context"
	"fmt"
	"strconv"
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
		// If it's a "not found" error, we might want to wrap it differently
		// For now, adhering to original logic which mock-failed on "999"
		return nil, fmt.Errorf("get user by id %q: %w", id, err)
	}

	span.SetAttributes(attribute.Bool("user.found", true))
	return user, nil
}

// GetProfile retrieves the current user's profile
// userID, username, email are passed from auth middleware (auth service token introspection)
func (s *UserService) GetProfile(ctx context.Context, userID string, username, email string) (*domain.User, error) {
	ctx, span := middleware.StartSpan(ctx, "user.profile", trace.WithAttributes(
		attribute.String("layer", "logic"),
		attribute.String("user.id", userID),
	))
	defer span.End()

	// Parse user_id
	uid, err := strconv.Atoi(userID)
	if err != nil {
		span.SetAttributes(attribute.Bool("profile.found", false))
		return nil, fmt.Errorf("invalid user_id %q: %w", userID, domain.ErrUserNotFound)
	}

	// Fetch profile from repository
	profile, err := s.repo.GetProfileByUserID(ctx, uid)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("query user profile: %w", err)
	}

	// If no profile found, return auth data (legacy/fallback behavior)
	if profile == nil {
		span.SetAttributes(attribute.Bool("profile.found", false))
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
	return user, nil
}

// CreateUser creates a new user profile
func (s *UserService) CreateUser(ctx context.Context, req domain.CreateUserRequest) (*domain.User, error) {
	ctx, span := middleware.StartSpan(ctx, "user.create", trace.WithAttributes(
		attribute.String("layer", "logic"),
		attribute.String("username", req.Username),
		attribute.String("email", req.Email),
	))
	defer span.End()

	// Validate email format
	if !strings.Contains(req.Email, "@") {
		span.SetAttributes(attribute.Bool("user.created", false))
		return nil, fmt.Errorf("validate email %q for user %q: %w", req.Email, req.Username, domain.ErrInvalidEmail)
	}

	// Mock production user_id logic (same as before)
	userID := len(req.Username) + 100

	// Check if profile exists
	exists, err := s.repo.CheckProfileExists(ctx, userID)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("check existing profile: %w", err)
	}
	if exists {
		span.SetAttributes(attribute.Bool("user.created", false))
		return nil, fmt.Errorf("create user %q: %w", req.Username, domain.ErrUserExists)
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

	// Create profile
	_, err = s.repo.CreateUserProfile(ctx, userID, firstName, lastName)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("insert user profile: %w", err)
	}

	user := &domain.User{
		ID:       strconv.Itoa(userID),
		Username: req.Username,
		Email:    req.Email,
		Name:     req.Name,
	}

	span.SetAttributes(
		attribute.String("user.id", user.ID),
		attribute.Bool("user.created", true),
	)
	span.AddEvent("user.created")

	return user, nil
}

// UpdateProfile updates the current user's profile
func (s *UserService) UpdateProfile(ctx context.Context, userID string, req domain.UpdateProfileRequest) (*domain.User, error) {
	ctx, span := middleware.StartSpan(ctx, "user.update_profile", trace.WithAttributes(
		attribute.String("layer", "logic"),
		attribute.String("user_id", userID),
	))
	defer span.End()

	// Parse user ID
	uid := 1
	if userID != "" {
		if parsed, err := strconv.Atoi(userID); err == nil {
			uid = parsed
		}
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
	err := s.repo.UpsertUserProfile(ctx, uid, firstName, lastName, req.Phone)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("upsert profile: %w", err)
	}

	user := &domain.User{
		ID:   strconv.Itoa(uid),
		Name: req.Name,
	}

	span.SetAttributes(attribute.Bool("profile.updated", true))
	return user, nil
}

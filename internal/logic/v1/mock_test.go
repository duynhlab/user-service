package v1

import (
	"context"

	"github.com/duynhlab/user-service/internal/core/domain"
)

// mockRepo is a configurable in-memory implementation of domain.UserRepository
// for table-driven tests. Each field holds the function invoked by the matching
// interface method, letting every test case wire up only the behavior it needs.
type mockRepo struct {
	getUserFn            func(ctx context.Context, id string) (*domain.User, error)
	getProfileByUserIDFn func(ctx context.Context, userID int) (*domain.UserProfile, error)
	createUserProfileFn  func(ctx context.Context, userID int, firstName, lastName string) (int, error)
	updateUserProfileFn  func(ctx context.Context, userID int, firstName, lastName, phone string) (bool, error)
	checkProfileExistsFn func(ctx context.Context, userID int) (bool, error)
	upsertUserProfileFn  func(ctx context.Context, userID int, firstName, lastName, phone string) error
}

func (m *mockRepo) GetUser(ctx context.Context, id string) (*domain.User, error) {
	return m.getUserFn(ctx, id)
}

func (m *mockRepo) GetProfileByUserID(ctx context.Context, userID int) (*domain.UserProfile, error) {
	return m.getProfileByUserIDFn(ctx, userID)
}

func (m *mockRepo) CreateUserProfile(ctx context.Context, userID int, firstName, lastName string) (int, error) {
	return m.createUserProfileFn(ctx, userID, firstName, lastName)
}

func (m *mockRepo) UpdateUserProfile(ctx context.Context, userID int, firstName, lastName, phone string) (bool, error) {
	return m.updateUserProfileFn(ctx, userID, firstName, lastName, phone)
}

func (m *mockRepo) CheckProfileExists(ctx context.Context, userID int) (bool, error) {
	return m.checkProfileExistsFn(ctx, userID)
}

func (m *mockRepo) UpsertUserProfile(ctx context.Context, userID int, firstName, lastName, phone string) error {
	return m.upsertUserProfileFn(ctx, userID, firstName, lastName, phone)
}

// strPtr returns a pointer to s, for populating UserProfile's optional fields.
func strPtr(s string) *string { return &s }

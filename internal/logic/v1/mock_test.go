package v1

import (
	"context"

	"github.com/duynhlab/user-service/internal/core/domain"
)

// mockRepo is a configurable in-memory implementation of domain.UserRepository
// for table-driven tests. Each field holds the function invoked by the matching
// interface method, letting every test case wire up only the behavior it needs.
type mockRepo struct {
	searchErr            error
	getUserFn            func(ctx context.Context, id string) (*domain.User, error)
	getProfileByUserIDFn func(ctx context.Context, userID string) (*domain.UserProfile, error)
	updateUserProfileFn  func(ctx context.Context, userID string, firstName, lastName, phone string) (bool, error)
	upsertUserProfileFn  func(ctx context.Context, userID string, firstName, lastName, phone string) error
}

func (m *mockRepo) GetUser(ctx context.Context, id string) (*domain.User, error) {
	return m.getUserFn(ctx, id)
}

func (m *mockRepo) GetProfileByUserID(ctx context.Context, userID string) (*domain.UserProfile, error) {
	return m.getProfileByUserIDFn(ctx, userID)
}

func (m *mockRepo) UpdateUserProfile(ctx context.Context, userID string, firstName, lastName, phone string) (bool, error) {
	return m.updateUserProfileFn(ctx, userID, firstName, lastName, phone)
}

func (m *mockRepo) UpsertUserProfile(ctx context.Context, userID string, firstName, lastName, phone string) error {
	return m.upsertUserProfileFn(ctx, userID, firstName, lastName, phone)
}

// strPtr returns a pointer to s, for populating UserProfile's optional fields.
func strPtr(s string) *string { return &s }

func (m *mockRepo) SearchProfiles(_ context.Context, _ string, _, _ int) ([]domain.UserProfile, int, error) {
	return nil, 0, m.searchErr
}

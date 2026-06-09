package v1

import (
	"context"
	"errors"
	"testing"

	"github.com/duynhlab/user-service/internal/core/domain"
)

var errRepo = errors.New("repo failure")

func TestUserService_GetUser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		id        string
		getUserFn func(ctx context.Context, id string) (*domain.User, error)
		wantName  string
		wantErr   error
	}{
		{
			name: "success",
			id:   "1",
			getUserFn: func(_ context.Context, id string) (*domain.User, error) {
				return &domain.User{ID: id, Name: "Alice Johnson"}, nil
			},
			wantName: "Alice Johnson",
		},
		{
			name: "not found",
			id:   "999",
			getUserFn: func(_ context.Context, _ string) (*domain.User, error) {
				return nil, domain.ErrUserNotFound
			},
			wantErr: domain.ErrUserNotFound,
		},
		{
			name: "repo error",
			id:   "1",
			getUserFn: func(_ context.Context, _ string) (*domain.User, error) {
				return nil, errRepo
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := NewUserService(&mockRepo{getUserFn: tt.getUserFn})

			got, err := svc.GetUser(context.Background(), tt.id)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("GetUser() error = %v, want %v", err, tt.wantErr)
				}
				if got != nil {
					t.Fatalf("GetUser() = %v, want nil on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetUser() unexpected error = %v", err)
			}
			if got.Name != tt.wantName {
				t.Errorf("GetUser() Name = %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

func TestUserService_GetProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		userID     string
		username   string
		email      string
		getProfile func(ctx context.Context, userID int) (*domain.UserProfile, error)
		wantName   string
		wantPhone  string
		wantErrIs  error
	}{
		{
			name:     "profile found with full name",
			userID:   "1",
			username: "alice",
			email:    "alice@example.com",
			getProfile: func(_ context.Context, _ int) (*domain.UserProfile, error) {
				return &domain.UserProfile{
					FirstName: strPtr("Alice"),
					LastName:  strPtr("Johnson"),
					Phone:     strPtr("+1-555-0101"),
				}, nil
			},
			wantName:  "Alice Johnson",
			wantPhone: "+1-555-0101",
		},
		{
			name:     "profile found but empty names falls back to default",
			userID:   "2",
			username: "bob",
			email:    "bob@example.com",
			getProfile: func(_ context.Context, _ int) (*domain.UserProfile, error) {
				return &domain.UserProfile{}, nil
			},
			wantName: "User 2",
		},
		{
			name:     "no profile returns auth fallback",
			userID:   "3",
			username: "carol",
			email:    "carol@example.com",
			getProfile: func(_ context.Context, _ int) (*domain.UserProfile, error) {
				return nil, nil
			},
			wantName: "User 3",
		},
		{
			name:     "invalid user id",
			userID:   "not-a-number",
			username: "x",
			email:    "x@example.com",
			// repo not consulted; nil fn would panic if called.
			wantErrIs: domain.ErrUserNotFound,
		},
		{
			name:     "repo error",
			userID:   "4",
			username: "dave",
			email:    "dave@example.com",
			getProfile: func(_ context.Context, _ int) (*domain.UserProfile, error) {
				return nil, errRepo
			},
			wantErrIs: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := NewUserService(&mockRepo{getProfileByUserIDFn: tt.getProfile})

			got, err := svc.GetProfile(context.Background(), tt.userID, tt.username, tt.email)

			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("GetProfile() error = %v, want %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetProfile() unexpected error = %v", err)
			}
			if got.Name != tt.wantName {
				t.Errorf("GetProfile() Name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Phone != tt.wantPhone {
				t.Errorf("GetProfile() Phone = %q, want %q", got.Phone, tt.wantPhone)
			}
			if got.Username != tt.username || got.Email != tt.email {
				t.Errorf("GetProfile() username/email = %q/%q, want %q/%q", got.Username, got.Email, tt.username, tt.email)
			}
		})
	}
}

func TestUserService_CreateUser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       domain.CreateUserRequest
		checkFn   func(ctx context.Context, userID int) (bool, error)
		createFn  func(ctx context.Context, userID int, firstName, lastName string) (int, error)
		wantID    string
		wantErrIs error
	}{
		{
			name: "success with first and last name",
			req:  domain.CreateUserRequest{UserID: 10, Username: "alice", Email: "alice@example.com", Name: "Alice Johnson"},
			checkFn: func(_ context.Context, _ int) (bool, error) {
				return false, nil
			},
			createFn: func(_ context.Context, _ int, firstName, lastName string) (int, error) {
				if firstName != "Alice" || lastName != "Johnson" {
					t.Errorf("CreateUserProfile got names %q/%q, want Alice/Johnson", firstName, lastName)
				}
				return 1, nil
			},
			wantID: "10",
		},
		{
			name:      "invalid email",
			req:       domain.CreateUserRequest{UserID: 10, Username: "alice", Email: "no-at-sign", Name: "Alice"},
			wantErrIs: domain.ErrInvalidEmail,
		},
		{
			name:      "invalid user id",
			req:       domain.CreateUserRequest{UserID: 0, Username: "alice", Email: "alice@example.com", Name: "Alice"},
			wantErrIs: domain.ErrInvalidUserID,
		},
		{
			name: "profile already exists",
			req:  domain.CreateUserRequest{UserID: 10, Username: "alice", Email: "alice@example.com", Name: "Alice"},
			checkFn: func(_ context.Context, _ int) (bool, error) {
				return true, nil
			},
			wantErrIs: domain.ErrUserExists,
		},
		{
			name: "check exists error",
			req:  domain.CreateUserRequest{UserID: 10, Username: "alice", Email: "alice@example.com", Name: "Alice"},
			checkFn: func(_ context.Context, _ int) (bool, error) {
				return false, errRepo
			},
			wantErrIs: errRepo,
		},
		{
			name: "create profile error",
			req:  domain.CreateUserRequest{UserID: 10, Username: "alice", Email: "alice@example.com", Name: "Alice"},
			checkFn: func(_ context.Context, _ int) (bool, error) {
				return false, nil
			},
			createFn: func(_ context.Context, _ int, _, _ string) (int, error) {
				return 0, errRepo
			},
			wantErrIs: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := NewUserService(&mockRepo{
				checkProfileExistsFn: tt.checkFn,
				createUserProfileFn:  tt.createFn,
			})

			got, err := svc.CreateUser(context.Background(), tt.req)

			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("CreateUser() error = %v, want %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateUser() unexpected error = %v", err)
			}
			if got.ID != tt.wantID {
				t.Errorf("CreateUser() ID = %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

func TestUserService_UpdateProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		userID    string
		req       domain.UpdateProfileRequest
		upsertFn  func(ctx context.Context, userID int, firstName, lastName, phone string) error
		wantName  string
		wantErrIs error
	}{
		{
			name:   "success",
			userID: "1",
			req:    domain.UpdateProfileRequest{Name: "Alice Johnson", Phone: "+1-555-0101"},
			upsertFn: func(_ context.Context, _ int, firstName, lastName, phone string) error {
				if firstName != "Alice" || lastName != "Johnson" || phone != "+1-555-0101" {
					t.Errorf("UpsertUserProfile got %q/%q/%q", firstName, lastName, phone)
				}
				return nil
			},
			wantName: "Alice Johnson",
		},
		{
			name:      "invalid user id",
			userID:    "abc",
			req:       domain.UpdateProfileRequest{Name: "Alice"},
			wantErrIs: domain.ErrUnauthorized,
		},
		{
			name:   "upsert error",
			userID: "1",
			req:    domain.UpdateProfileRequest{Name: "Alice"},
			upsertFn: func(_ context.Context, _ int, _, _, _ string) error {
				return errRepo
			},
			wantErrIs: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := NewUserService(&mockRepo{upsertUserProfileFn: tt.upsertFn})

			got, err := svc.UpdateProfile(context.Background(), tt.userID, tt.req)

			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("UpdateProfile() error = %v, want %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateProfile() unexpected error = %v", err)
			}
			if got.Name != tt.wantName {
				t.Errorf("UpdateProfile() Name = %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

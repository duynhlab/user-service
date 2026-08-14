package v1

import (
	"context"
	"errors"
	"testing"

	"github.com/duynhlab/user-service/internal/core/domain"
)

var errRepo = errors.New("repo failure")

// aliceSub is the fixed realm subject of the alice demo user (Keycloak realm
// import) — user ids are opaque OIDC subject strings (ADR-041).
const aliceSub = "a11ce000-0000-4000-8000-000000000001"

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
			id:   aliceSub,
			getUserFn: func(_ context.Context, id string) (*domain.User, error) {
				return &domain.User{ID: id, Name: "Alice Johnson"}, nil
			},
			wantName: "Alice Johnson",
		},
		{
			name: "not found",
			id:   "00000000-0000-4000-8000-000000000999",
			getUserFn: func(_ context.Context, _ string) (*domain.User, error) {
				return nil, domain.ErrUserNotFound
			},
			wantErr: domain.ErrUserNotFound,
		},
		{
			name: "repo error",
			id:   aliceSub,
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
		getProfile func(ctx context.Context, userID string) (*domain.UserProfile, error)
		wantName   string
		wantPhone  string
		wantErrIs  error
	}{
		{
			name:     "profile found with full name",
			userID:   aliceSub,
			username: "alice",
			email:    "alice@example.com",
			getProfile: func(_ context.Context, _ string) (*domain.UserProfile, error) {
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
			userID:   "a11ce000-0000-4000-8000-000000000002",
			username: "bob",
			email:    "bob@example.com",
			getProfile: func(_ context.Context, _ string) (*domain.UserProfile, error) {
				return &domain.UserProfile{}, nil
			},
			wantName: "User a11ce000-0000-4000-8000-000000000002",
		},
		{
			name:     "no profile row returns JIT claims fallback",
			userID:   "a11ce000-0000-4000-8000-000000000003",
			username: "carol",
			email:    "carol@example.com",
			getProfile: func(_ context.Context, _ string) (*domain.UserProfile, error) {
				return nil, nil
			},
			wantName: "User a11ce000-0000-4000-8000-000000000003",
		},
		{
			name:     "empty subject rejected",
			userID:   "",
			username: "x",
			email:    "x@example.com",
			// repo not consulted; nil fn would panic if called.
			wantErrIs: domain.ErrUserNotFound,
		},
		{
			name:     "repo error",
			userID:   "a11ce000-0000-4000-8000-000000000004",
			username: "david",
			email:    "david@example.com",
			getProfile: func(_ context.Context, _ string) (*domain.UserProfile, error) {
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

// TestUserService_GetProfile_JITFallbackClaimsOnly pins the JIT provisioning
// read: with no profile row the identity is built from the verified token
// claims only (subject, username, email) — no synthesized or persisted data.
func TestUserService_GetProfile_JITFallbackClaimsOnly(t *testing.T) {
	t.Parallel()
	svc := NewUserService(&mockRepo{
		getProfileByUserIDFn: func(_ context.Context, _ string) (*domain.UserProfile, error) {
			return nil, nil
		},
	})

	got, err := svc.GetProfile(context.Background(), aliceSub, "alice", "alice@example.com")
	if err != nil {
		t.Fatalf("GetProfile() unexpected error = %v", err)
	}
	if got.ID != aliceSub {
		t.Errorf("ID = %q, want token subject %q", got.ID, aliceSub)
	}
	if got.Username != "alice" || got.Email != "alice@example.com" {
		t.Errorf("username/email = %q/%q, want claims alice/alice@example.com", got.Username, got.Email)
	}
	if got.Phone != "" {
		t.Errorf("Phone = %q, want empty (no stored profile)", got.Phone)
	}
}

func TestUserService_UpdateProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		userID    string
		req       domain.UpdateProfileRequest
		upsertFn  func(ctx context.Context, userID string, firstName, lastName, phone string) error
		wantName  string
		wantErrIs error
	}{
		{
			name:   "success upserts with subject string",
			userID: aliceSub,
			req:    domain.UpdateProfileRequest{Name: "Alice Johnson", Phone: "+1-555-0101"},
			upsertFn: func(_ context.Context, userID string, firstName, lastName, phone string) error {
				if userID != aliceSub {
					t.Errorf("UpsertUserProfile got userID %q, want %q", userID, aliceSub)
				}
				if firstName != "Alice" || lastName != "Johnson" || phone != "+1-555-0101" {
					t.Errorf("UpsertUserProfile got %q/%q/%q", firstName, lastName, phone)
				}
				return nil
			},
			wantName: "Alice Johnson",
		},
		{
			name:      "empty subject rejected",
			userID:    "",
			req:       domain.UpdateProfileRequest{Name: "Alice"},
			wantErrIs: domain.ErrUnauthorized,
		},
		{
			name:   "upsert error",
			userID: aliceSub,
			req:    domain.UpdateProfileRequest{Name: "Alice"},
			upsertFn: func(_ context.Context, _ string, _, _, _ string) error {
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

func TestSearchProfilesPassThrough(t *testing.T) {
	m := &mockRepo{}
	svc := svcWith(m)
	if _, _, err := svc.SearchProfiles(context.Background(), "ali", 20, 0); err != nil {
		t.Fatalf("search: %v", err)
	}
	// error branch: preserved and wrapped
	bad := svcWith(&mockRepo{searchErr: context.DeadlineExceeded})
	if _, _, err := bad.SearchProfiles(context.Background(), "x", 20, 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("search error not preserved: %v", err)
	}
}

func TestGetProfileRecord(t *testing.T) {
	m := &mockRepo{getProfileByUserIDFn: func(_ context.Context, _ string) (*domain.UserProfile, error) {
		return &domain.UserProfile{UserID: "u-1"}, nil
	}}
	if p, err := svcWith(m).GetProfileRecord(context.Background(), "u-1"); err != nil || p.UserID != "u-1" {
		t.Fatalf("record: %v %v", p, err)
	}
	m2 := &mockRepo{getProfileByUserIDFn: func(_ context.Context, _ string) (*domain.UserProfile, error) {
		return nil, nil
	}}
	if _, err := svcWith(m2).GetProfileRecord(context.Background(), "u-x"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("want ErrUserNotFound, got %v", err)
	}
}

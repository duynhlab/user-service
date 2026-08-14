package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/duynhlab/user-service/internal/core/domain"
	logicv1 "github.com/duynhlab/user-service/internal/logic/v1"
	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// aliceSub is the fixed realm subject of the alice demo user — user ids are
// opaque OIDC subject strings (ADR-041).
const aliceSub = "a11ce000-0000-4000-8000-000000000001"

// mockUserRepo is a configurable domain.UserRepository double for web tests.
// Each field holds the function invoked by the matching interface method, so a
// test wires up only the behavior it needs.
type mockUserRepo struct {
	getUserFn            func(ctx context.Context, id string) (*domain.User, error)
	getProfileByUserIDFn func(ctx context.Context, userID string) (*domain.UserProfile, error)
	updateUserProfileFn  func(ctx context.Context, userID string, firstName, lastName, phone string) (bool, error)
	upsertUserProfileFn  func(ctx context.Context, userID string, firstName, lastName, phone string) error
}

func (m *mockUserRepo) GetUser(ctx context.Context, id string) (*domain.User, error) {
	return m.getUserFn(ctx, id)
}
func (m *mockUserRepo) GetProfileByUserID(ctx context.Context, userID string) (*domain.UserProfile, error) {
	return m.getProfileByUserIDFn(ctx, userID)
}
func (m *mockUserRepo) UpdateUserProfile(ctx context.Context, userID string, firstName, lastName, phone string) (bool, error) {
	return m.updateUserProfileFn(ctx, userID, firstName, lastName, phone)
}
func (m *mockUserRepo) UpsertUserProfile(ctx context.Context, userID string, firstName, lastName, phone string) error {
	return m.upsertUserProfileFn(ctx, userID, firstName, lastName, phone)
}

func (m *mockUserRepo) SearchProfiles(_ context.Context, _ string, _, _ int) ([]domain.UserProfile, int, error) {
	return nil, 0, nil
}

func newHandler(repo domain.UserRepository) *UserHandler {
	return NewUserHandler(logicv1.NewUserService(repo))
}

// newCtx builds a gin context with a no-body request, optional auth keys, and
// route params. authKeys sets c.Set values (e.g. user_id, username, email).
func newCtx(method, target string, authKeys map[string]string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, target, nil)
	for k, v := range authKeys {
		c.Set(k, v)
	}
	c.Params = params
	return c, rec
}

// ctxWithBody is newCtx with a JSON request body.
func ctxWithBody(method, target, body string, authKeys map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	for k, v := range authKeys {
		c.Set(k, v)
	}
	return c, rec
}

// decode returns the parsed JSON body.
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body %q: %v", rec.Body.String(), err)
	}
	return body
}

// --- GetUser ---

func TestGetUser_Success(t *testing.T) {
	repo := &mockUserRepo{
		getUserFn: func(_ context.Context, id string) (*domain.User, error) {
			return &domain.User{ID: id, Name: "Alice", Email: "a@b.com"}, nil
		},
	}
	c, rec := newCtx(http.MethodGet, "/user/v1/public/users/"+aliceSub, nil, gin.Params{{Key: "id", Value: aliceSub}})
	newHandler(repo).GetUser(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	if body["name"] != "Alice" || body["id"] != aliceSub {
		t.Errorf("body = %v, want id=%s name=Alice", body, aliceSub)
	}
	// PublicUser must not leak email.
	if _, ok := body["email"]; ok {
		t.Errorf("response leaked email: %s", rec.Body.String())
	}
}

func TestGetUser_NotFound(t *testing.T) {
	repo := &mockUserRepo{
		getUserFn: func(_ context.Context, _ string) (*domain.User, error) {
			return nil, domain.ErrUserNotFound
		},
	}
	c, rec := newCtx(http.MethodGet, "/user/v1/public/users/unknown-sub", nil, gin.Params{{Key: "id", Value: "unknown-sub"}})
	newHandler(repo).GetUser(c)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if code := decode(t, rec)["code"]; code != "NOT_FOUND" {
		t.Errorf("code = %v, want NOT_FOUND", code)
	}
}

func TestGetUser_InternalError(t *testing.T) {
	repo := &mockUserRepo{
		getUserFn: func(_ context.Context, _ string) (*domain.User, error) {
			return nil, errors.New("db down")
		},
	}
	c, rec := newCtx(http.MethodGet, "/user/v1/public/users/"+aliceSub, nil, gin.Params{{Key: "id", Value: aliceSub}})
	newHandler(repo).GetUser(c)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if code := decode(t, rec)["code"]; code != "INTERNAL_ERROR" {
		t.Errorf("code = %v, want INTERNAL_ERROR", code)
	}
}

// --- GetProfile ---

func TestGetProfile_Success(t *testing.T) {
	repo := &mockUserRepo{
		getProfileByUserIDFn: func(_ context.Context, _ string) (*domain.UserProfile, error) {
			fn, ln, ph := "Jane", "Doe", "555"
			return &domain.UserProfile{FirstName: &fn, LastName: &ln, Phone: &ph}, nil
		},
	}
	c, rec := newCtx(http.MethodGet, "/user/v1/private/users/profile",
		map[string]string{"user_id": aliceSub, "username": "jane", "email": "j@d.com"}, nil)
	newHandler(repo).GetProfile(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	if body["name"] != "Jane Doe" {
		t.Errorf("name = %v, want Jane Doe", body["name"])
	}
}

// JIT provisioning read: no profile row yet — the handler still returns 200
// with an identity built from the verified token claims only.
func TestGetProfile_JITFallback(t *testing.T) {
	repo := &mockUserRepo{
		getProfileByUserIDFn: func(_ context.Context, _ string) (*domain.UserProfile, error) {
			return nil, nil
		},
	}
	c, rec := newCtx(http.MethodGet, "/user/v1/private/users/profile",
		map[string]string{"user_id": aliceSub, "username": "alice", "email": "alice@example.com"}, nil)
	newHandler(repo).GetProfile(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	if body["id"] != aliceSub || body["username"] != "alice" || body["email"] != "alice@example.com" {
		t.Errorf("body = %v, want claims identity id=%s username=alice email=alice@example.com", body, aliceSub)
	}
}

func TestGetProfile_Unauthorized(t *testing.T) {
	c, rec := newCtx(http.MethodGet, "/user/v1/private/users/profile", nil, nil)
	newHandler(&mockUserRepo{}).GetProfile(c)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if code := decode(t, rec)["code"]; code != "UNAUTHORIZED" {
		t.Errorf("code = %v, want UNAUTHORIZED", code)
	}
}

func TestGetProfile_InternalError(t *testing.T) {
	repo := &mockUserRepo{
		getProfileByUserIDFn: func(_ context.Context, _ string) (*domain.UserProfile, error) {
			return nil, errors.New("db down")
		},
	}
	c, rec := newCtx(http.MethodGet, "/user/v1/private/users/profile",
		map[string]string{"user_id": aliceSub}, nil)
	newHandler(repo).GetProfile(c)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if code := decode(t, rec)["code"]; code != "INTERNAL_ERROR" {
		t.Errorf("code = %v, want INTERNAL_ERROR", code)
	}
}

// --- UpdateProfile ---

func TestUpdateProfile_Success(t *testing.T) {
	repo := &mockUserRepo{
		upsertUserProfileFn: func(_ context.Context, _ string, _, _, _ string) error { return nil },
	}
	c, rec := ctxWithBody(http.MethodPut, "/user/v1/private/users/profile",
		`{"name":"New Name","phone":"555"}`, map[string]string{"user_id": aliceSub})
	newHandler(repo).UpdateProfile(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if decode(t, rec)["name"] != "New Name" {
		t.Errorf("name = %v, want New Name", decode(t, rec)["name"])
	}
}

func TestUpdateProfile_Unauthorized(t *testing.T) {
	c, rec := ctxWithBody(http.MethodPut, "/user/v1/private/users/profile", `{"name":"x"}`, nil)
	newHandler(&mockUserRepo{}).UpdateProfile(c)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if code := decode(t, rec)["code"]; code != "UNAUTHORIZED" {
		t.Errorf("code = %v, want UNAUTHORIZED", code)
	}
}

func TestUpdateProfile_BadJSON(t *testing.T) {
	c, rec := ctxWithBody(http.MethodPut, "/user/v1/private/users/profile", "{",
		map[string]string{"user_id": aliceSub})
	newHandler(&mockUserRepo{}).UpdateProfile(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if code := decode(t, rec)["code"]; code != "VALIDATION_ERROR" {
		t.Errorf("code = %v, want VALIDATION_ERROR", code)
	}
}

// A logic-layer ErrUnauthorized (e.g. subject/ownership mismatch) maps to 403.
func TestUpdateProfile_Forbidden(t *testing.T) {
	repo := &mockUserRepo{
		upsertUserProfileFn: func(_ context.Context, _ string, _, _, _ string) error {
			return domain.ErrUnauthorized
		},
	}
	c, rec := ctxWithBody(http.MethodPut, "/user/v1/private/users/profile", `{"name":"x"}`,
		map[string]string{"user_id": aliceSub})
	newHandler(repo).UpdateProfile(c)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if code := decode(t, rec)["code"]; code != "FORBIDDEN" {
		t.Errorf("code = %v, want FORBIDDEN", code)
	}
}

func TestUpdateProfile_InternalError(t *testing.T) {
	repo := &mockUserRepo{
		upsertUserProfileFn: func(_ context.Context, _ string, _, _, _ string) error { return errors.New("db down") },
	}
	c, rec := ctxWithBody(http.MethodPut, "/user/v1/private/users/profile", `{"name":"x"}`,
		map[string]string{"user_id": aliceSub})
	newHandler(repo).UpdateProfile(c)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if code := decode(t, rec)["code"]; code != "INTERNAL_ERROR" {
		t.Errorf("code = %v, want INTERNAL_ERROR", code)
	}
}

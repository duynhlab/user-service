package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/duynhlab/pkg/authmw"
	"github.com/duynhlab/user-service/internal/core/domain"
	logicv1 "github.com/duynhlab/user-service/internal/logic/v1"
)

func strp(s string) *string { return &s }

// searchRepo scripts the widened repository for the protected surface.
type searchRepo struct {
	items     []domain.UserProfile
	total     int
	searchErr error
	detailErr error
	got       struct {
		query         string
		limit, offset int
	}
	profile *domain.UserProfile
}

func (m *searchRepo) GetUser(_ context.Context, _ string) (*domain.User, error) { return nil, nil }
func (m *searchRepo) GetProfileByUserID(_ context.Context, _ string) (*domain.UserProfile, error) {
	return m.profile, m.detailErr
}
func (m *searchRepo) UpdateUserProfile(_ context.Context, _ string, _, _, _ string) (bool, error) {
	return false, nil
}
func (m *searchRepo) UpsertUserProfile(_ context.Context, _ string, _, _, _ string) error {
	return nil
}
func (m *searchRepo) SearchProfiles(_ context.Context, query string, limit, offset int) ([]domain.UserProfile, int, error) {
	m.got.query, m.got.limit, m.got.offset = query, limit, offset
	return m.items, m.total, m.searchErr
}

func protectedEngine(t *testing.T, repo domain.UserRepository, roles ...string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewUserHandler(logicv1.NewUserService(repo))
	h.mountProtected(r,
		func(c *gin.Context) {
			c.Set(authmw.CtxUserID, "d0e00000-0000-4000-8000-000000000001")
			c.Set(authmw.CtxRoles, roles)
			c.Next()
		},
		authmw.MiddlewareRequireRole(backofficeRole))
	return r
}

func TestProtectedUsersRoleGate(t *testing.T) {
	r := protectedEngine(t, &searchRepo{}, "customer")
	for _, path := range []string{"/user/v1/protected/users", "/user/v1/protected/users/u-1"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: want 403, got %d", path, w.Code)
		}
	}
}

func TestSearchUsersShapesTheListSafely(t *testing.T) {
	repo := &searchRepo{
		items: []domain.UserProfile{{
			UserID: "a11ce000-0000-4000-8000-000000000001", FirstName: strp("Alice"),
			LastName: strp("Johnson"), Phone: strp("555-1234"),
			Address: strp("13 Hidden Lane"), CreatedAt: time.Unix(0, 0).UTC(),
		}},
		total: 1,
	}
	r := protectedEngine(t, repo, backofficeRole)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/user/v1/protected/users?query=ali&page=1&page_size=20", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.got.query != "ali" || repo.got.limit != 20 || repo.got.offset != 0 {
		t.Fatalf("search args not forwarded: %+v", repo.got)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"name":"Alice Johnson"`) {
		t.Fatalf("display name not composed: %s", body)
	}
	// The LIST never leaks the address — that is a detail-view field.
	if strings.Contains(body, "Hidden Lane") {
		t.Fatalf("address leaked into the list payload: %s", body)
	}
}

func TestGetUserDetail(t *testing.T) {
	repo := &searchRepo{profile: &domain.UserProfile{
		UserID: "u-7", FirstName: strp("Carol"), Address: strp("42 Fulfillment Way"),
		CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC(),
	}}
	r := protectedEngine(t, repo, backofficeRole)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/user/v1/protected/users/u-7", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Address string `json:"address"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Address != "42 Fulfillment Way" {
		t.Fatalf("detail must include the address, got %q", resp.Address)
	}

	repo.profile = nil
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/user/v1/protected/users/u-missing", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestProtectedUsersErrorBranches(t *testing.T) {
	r := protectedEngine(t, &searchRepo{searchErr: context.DeadlineExceeded, detailErr: context.DeadlineExceeded}, backofficeRole)
	for _, path := range []string{"/user/v1/protected/users", "/user/v1/protected/users/u-1"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("%s: want 500, got %d", path, w.Code)
		}
	}
}

func TestRegisterProtectedRoutesRealChain(t *testing.T) {
	verifier, err := authmw.NewVerifier(authmw.Config{
		Issuer:   "http://localhost:8081/realms/duynhlab-staff",
		Audience: "duynhlab-platform",
	})
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterProtectedRoutes(r, NewUserHandler(logicv1.NewUserService(&searchRepo{})), verifier)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/user/v1/protected/users", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("tokenless: want 401 from the real chain, got %d", w.Code)
	}
}

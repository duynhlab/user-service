package v1

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/duynhlab/pkg/authmw"
	"github.com/duynhlab/pkg/httpx"
	"github.com/duynhlab/user-service/internal/core/domain"
)

// Protected Backoffice reads (RFC-0023 slice A, ADR-047/050): operator
// search + case view over user_profiles. The staff-realm verifier is
// authoritative; every route requires backoffice_admin. Identity claims
// (email/username) live in Keycloak, not this service — name/phone/user_id
// is the entire searchable surface, and that is documented behavior, not a
// gap. The customer-facing private group keeps its own (customer-realm)
// verifier untouched.

// backofficeRole is the staff-realm role every protected route requires.
const backofficeRole = "backoffice_admin"

// RegisterProtectedRoutes mounts the Backoffice group with the real guard
// chain. Split from mountProtected so tests can inject fakes.
func RegisterProtectedRoutes(r *gin.Engine, h *UserHandler, staffVerifier *authmw.Verifier) {
	h.mountProtected(r, authmw.MiddlewareJWT(staffVerifier), authmw.MiddlewareRequireRole(backofficeRole))
}

func (h *UserHandler) mountProtected(r *gin.Engine, authMW ...gin.HandlerFunc) {
	protected := r.Group("/user/v1/protected")
	protected.Use(authMW...)
	{
		protected.GET("/users", h.SearchUsers)
		protected.GET("/users/:userId", h.GetUserDetail)
	}
}

// profileListJSON is the operator LIST shape: no address (sensitive fields
// stay out of list pages by default — RFC-0023 security considerations).
type profileListJSON struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	CreatedAt string `json:"created_at"`
}

// profileDetailJSON is the case view: address included — the operator's
// legitimate fulfillment need — behind the explicit detail request.
type profileDetailJSON struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	Address   string `json:"address"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func displayName(p domain.UserProfile) string {
	name := deref(p.FirstName)
	if last := deref(p.LastName); last != "" {
		if name != "" {
			name += " "
		}
		name += last
	}
	return name
}

// SearchUsers serves GET /users?query=&page=&page_size=.
func (h *UserHandler) SearchUsers(c *gin.Context) {
	page, pageSize := httpx.ParsePage(c)

	items, total, err := h.service.SearchProfiles(c.Request.Context(), c.Query("query"), pageSize, httpx.Offset(page, pageSize))
	if err != nil {
		httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		return
	}
	out := make([]profileListJSON, 0, len(items))
	for _, p := range items {
		out = append(out, profileListJSON{
			UserID:    p.UserID,
			Name:      displayName(p),
			Phone:     deref(p.Phone),
			CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, httpx.NewPaginated(out, page, pageSize, total))
}

// GetUserDetail serves GET /users/:userId — the operator case view.
func (h *UserHandler) GetUserDetail(c *gin.Context) {
	profile, err := h.service.GetProfileRecord(c.Request.Context(), c.Param("userId"))
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			httpx.RespondError(c, http.StatusNotFound, httpx.CodeNotFound, "User has no profile")
			return
		}
		httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		return
	}
	c.JSON(http.StatusOK, profileDetailJSON{
		UserID:    profile.UserID,
		Name:      displayName(*profile),
		Phone:     deref(profile.Phone),
		Address:   deref(profile.Address),
		CreatedAt: profile.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: profile.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

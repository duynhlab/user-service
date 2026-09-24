package v1

import (
	"context"
	"errors"
	"net/http"

	"github.com/duynhlab/pkg/httpx"
	"github.com/duynhlab/pkg/logger/slogx"
	"github.com/duynhlab/user-service/internal/core/domain"
	logicv1 "github.com/duynhlab/user-service/internal/logic/v1"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// PublicUser is the minimal, public-safe view of a user returned by the
// public GET /user/v1/public/users/:id endpoint. It deliberately omits email
// and other sensitive fields.
type PublicUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UserHandler handles HTTP requests for user operations
type UserHandler struct {
	service *logicv1.UserService
}

// NewUserHandler creates a new user handler
func NewUserHandler(service *logicv1.UserService) *UserHandler {
	return &UserHandler{
		service: service,
	}
}

// beginRequest resolves the otelgin server span. The web layer does not mint
// its own span — otelgin already opened the server span for this request
// (method/route are on it), so handlers annotate that span via the returned
// handle. The caller must NOT end it; otelgin owns its lifecycle. Records are
// written with the returned context, which is what correlates them.
func beginRequest(c *gin.Context) (context.Context, trace.Span) {
	ctx := c.Request.Context()
	return ctx, trace.SpanFromContext(ctx)
}

// GetUser handles HTTP request to get a user by ID
func (h *UserHandler) GetUser(c *gin.Context) {
	ctx, span := beginRequest(c)

	id := c.Param("id")
	span.SetAttributes(attribute.String("user.id", id))

	user, err := h.service.GetUser(ctx, id)
	if err != nil {
		span.RecordError(err)
		slogx.FromContext(ctx).Error(ctx, "Failed to get user", slogx.Err(err))

		switch {
		case errors.Is(err, domain.ErrUserNotFound):
			httpx.RespondError(c, http.StatusNotFound, httpx.CodeNotFound, "User not found")
		default:
			httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		}
		return
	}

	slogx.FromContext(ctx).Info(ctx, "User retrieved")
	c.JSON(http.StatusOK, PublicUser{ID: user.ID, Name: user.Name})
}

// GetProfile handles HTTP request to get current user profile
func (h *UserHandler) GetProfile(c *gin.Context) {
	ctx, span := beginRequest(c)

	// Extract user info from auth middleware context (required - no fallback)
	userID := c.GetString("user_id")
	if userID == "" {
		slogx.FromContext(ctx).Warn(ctx, "GetProfile: no user_id in context")
		httpx.RespondError(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Authentication required")
		return
	}
	username := c.GetString("username")
	email := c.GetString("email")

	user, err := h.service.GetProfile(ctx, userID, username, email)
	if err != nil {
		respondProfileError(c, span, "Failed to get profile", err)
		return
	}

	slogx.FromContext(ctx).Info(ctx, "Profile retrieved")
	c.JSON(http.StatusOK, user)
}

// UpdateProfile handles PUT /user/v1/private/users/profile
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	ctx, span := beginRequest(c)

	// Get user_id from auth middleware (required - no fallback)
	userID := c.GetString("user_id")
	if userID == "" {
		slogx.FromContext(ctx).Warn(ctx, "UpdateProfile: no user_id in context")
		httpx.RespondError(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Authentication required")
		return
	}

	var req domain.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		span.SetAttributes(attribute.Bool("request.valid", false))
		span.RecordError(err)
		slogx.FromContext(ctx).Warn(ctx, "Invalid request", slogx.Err(err))
		httpx.RespondError(c, http.StatusBadRequest, httpx.CodeValidation, sanitizeValidationError(err))
		return
	}

	span.SetAttributes(attribute.Bool("request.valid", true))

	user, err := h.service.UpdateProfile(ctx, userID, req)
	if err != nil {
		respondProfileError(c, span, "Failed to update profile", err)
		return
	}

	slogx.FromContext(ctx).Info(ctx, "Profile updated")
	c.JSON(http.StatusOK, user)
}

// respondProfileError records a profile operation's failure on the span and
// the log, and answers 403 for an ownership refusal, 500 otherwise.
func respondProfileError(c *gin.Context, span trace.Span, msg string, err error) {
	ctx := c.Request.Context()
	span.RecordError(err)
	slogx.FromContext(ctx).Error(ctx, msg, slogx.Err(err))
	if errors.Is(err, domain.ErrUnauthorized) {
		httpx.RespondError(c, http.StatusForbidden, httpx.CodeForbidden, "Unauthorized access")
		return
	}
	httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
}

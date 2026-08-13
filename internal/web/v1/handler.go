package v1

import (
	"context"
	"errors"
	"net/http"

	"github.com/duynhlab/pkg/httpx"
	"github.com/duynhlab/user-service/internal/core/domain"
	logicv1 "github.com/duynhlab/user-service/internal/logic/v1"
	"github.com/duynhlab/user-service/middleware"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
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

// beginRequest resolves the otelgin server span and the request logger. The web
// layer does not mint its own span — otelgin already opened the server span for
// this request (method/route are on it), so handlers annotate that span via the
// returned handle. The caller must NOT end it; otelgin owns its lifecycle.
func beginRequest(c *gin.Context) (context.Context, trace.Span, *zap.Logger) {
	ctx := c.Request.Context()
	return ctx, trace.SpanFromContext(ctx), middleware.GetLoggerFromGinContext(c)
}

// GetUser handles HTTP request to get a user by ID
func (h *UserHandler) GetUser(c *gin.Context) {
	ctx, span, zapLogger := beginRequest(c)

	id := c.Param("id")
	span.SetAttributes(attribute.String("user.id", id))

	user, err := h.service.GetUser(ctx, id)
	if err != nil {
		span.RecordError(err)
		zapLogger.Error("Failed to get user", zap.Error(err))

		switch {
		case errors.Is(err, domain.ErrUserNotFound):
			httpx.RespondError(c, http.StatusNotFound, httpx.CodeNotFound, "User not found")
		default:
			httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		}
		return
	}

	zapLogger.Info("User retrieved", zap.String("user_id", id))
	c.JSON(http.StatusOK, PublicUser{ID: user.ID, Name: user.Name})
}

// GetProfile handles HTTP request to get current user profile
func (h *UserHandler) GetProfile(c *gin.Context) {
	ctx, span, zapLogger := beginRequest(c)

	// Extract user info from auth middleware context (required - no fallback)
	userID := c.GetString("user_id")
	if userID == "" {
		zapLogger.Warn("GetProfile: no user_id in context")
		httpx.RespondError(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Authentication required")
		return
	}
	username := c.GetString("username")
	email := c.GetString("email")

	user, err := h.service.GetProfile(ctx, userID, username, email)
	if err != nil {
		span.RecordError(err)
		zapLogger.Error("Failed to get profile", zap.Error(err))

		switch {
		case errors.Is(err, domain.ErrUnauthorized):
			httpx.RespondError(c, http.StatusForbidden, httpx.CodeForbidden, "Unauthorized access")
		default:
			httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		}
		return
	}

	zapLogger.Info("Profile retrieved")
	c.JSON(http.StatusOK, user)
}

// UpdateProfile handles PUT /user/v1/private/users/profile
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	ctx, span, zapLogger := beginRequest(c)

	// Get user_id from auth middleware (required - no fallback)
	userID := c.GetString("user_id")
	if userID == "" {
		zapLogger.Warn("UpdateProfile: no user_id in context")
		httpx.RespondError(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Authentication required")
		return
	}

	var req domain.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		span.SetAttributes(attribute.Bool("request.valid", false))
		span.RecordError(err)
		zapLogger.Error("Invalid request", zap.Error(err))
		httpx.RespondError(c, http.StatusBadRequest, httpx.CodeValidation, sanitizeValidationError(err))
		return
	}

	span.SetAttributes(attribute.Bool("request.valid", true))

	user, err := h.service.UpdateProfile(ctx, userID, req)
	if err != nil {
		span.RecordError(err)
		zapLogger.Error("Failed to update profile", zap.Error(err))

		switch {
		case errors.Is(err, domain.ErrUnauthorized):
			httpx.RespondError(c, http.StatusForbidden, httpx.CodeForbidden, "Unauthorized access")
		default:
			httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		}
		return
	}

	zapLogger.Info("Profile updated", zap.String("user_id", userID))
	c.JSON(http.StatusOK, user)
}

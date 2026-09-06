// File: internal/modules/users/api/user_handler.go

package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/service"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/middleware"
)

// UserHandler is the transport layer for the account resource.
type UserHandler struct {
	users *service.UserService
}

// NewUserHandler builds the handler.
func NewUserHandler(users *service.UserService) *UserHandler {
	return &UserHandler{users: users}
}

// Me returns the authenticated caller's own account.
func (h *UserHandler) Me(c *gin.Context) {
	var id uuid.UUID
	var ok bool
	id, ok = middleware.UserIDFrom(c)
	if !ok {
		handleError(c, errors.NewUnauthorizedError("authentication required", nil))
		return
	}

	var result *service.UserResponseDTO
	var err error
	result, err = h.users.GetByID(c.Request.Context(), id)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateMe applies a partial update to the caller's own account.
func (h *UserHandler) UpdateMe(c *gin.Context) {
	var id uuid.UUID
	var ok bool
	id, ok = middleware.UserIDFrom(c)
	if !ok {
		handleError(c, errors.NewUnauthorizedError("authentication required", nil))
		return
	}

	var req service.UpdateProfileRequestDTO
	if !bindJSON(c, &req) {
		return
	}

	var result *service.UserResponseDTO
	var err error
	result, err = h.users.UpdateProfile(c.Request.Context(), id, &req)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ChangePassword replaces the caller's own password.
func (h *UserHandler) ChangePassword(c *gin.Context) {
	var id uuid.UUID
	var ok bool
	id, ok = middleware.UserIDFrom(c)
	if !ok {
		handleError(c, errors.NewUnauthorizedError("authentication required", nil))
		return
	}

	var req service.ChangePasswordRequestDTO
	if !bindJSON(c, &req) {
		return
	}

	var err error = h.users.ChangePassword(c.Request.Context(), id, &req)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "password updated"})
}

// GetByID returns one account by ID.
func (h *UserHandler) GetByID(c *gin.Context) {
	var id uuid.UUID
	var err error
	id, err = uuid.Parse(c.Param("id"))
	if err != nil {
		handleError(c, errors.NewBadRequestError("that is not a valid user id", err))
		return
	}

	var result *service.UserResponseDTO
	result, err = h.users.GetByID(c.Request.Context(), id)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// List returns a page of accounts.
func (h *UserHandler) List(c *gin.Context) {
	var result *service.UserListResponseDTO
	var err error
	result, err = h.users.List(c.Request.Context(), pageFromQuery(c))
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateStatus is the administrative enable/disable.
func (h *UserHandler) UpdateStatus(c *gin.Context) {
	var id uuid.UUID
	var err error
	id, err = uuid.Parse(c.Param("id"))
	if err != nil {
		handleError(c, errors.NewBadRequestError("that is not a valid user id", err))
		return
	}

	var req service.UpdateStatusRequestDTO
	if !bindJSON(c, &req) {
		return
	}

	var result *service.UserResponseDTO
	result, err = h.users.UpdateStatus(c.Request.Context(), id, domain.UserStatus(req.Status))
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// pageFromQuery reads limit and offset.
//
// An unparseable value falls back to the default rather than failing the
// request: ?limit=abc is a client mistake with an obvious safe reading, and the
// page bounds are clamped by domain.Page.Normalize regardless.
func pageFromQuery(c *gin.Context) domain.Page {
	var page domain.Page
	page.Limit, _ = strconv.Atoi(c.DefaultQuery("limit", "0"))
	page.Offset, _ = strconv.Atoi(c.DefaultQuery("offset", "0"))
	return page.Normalize()
}

// handleError asks the error for its status. See the auth handler for the full
// rationale; this is the same function, and the duplication is deliberate —
// each module's api package owns its rendering.
func handleError(c *gin.Context, err error) {
	// The request ID is attached here so every error response carries it,
	// whichever layer produced the error — it is what ties a support report to
	// a line in the log.
	var appErr *errors.AppError = errors.From(err).
		WithRequestID(middleware.RequestIDFrom(c))
	c.JSON(appErr.ToHTTPStatus(), appErr.Response())
}

// bindJSON decodes the body, reporting whether it succeeded.
func bindJSON(c *gin.Context, target any) bool {
	var err error = c.ShouldBindJSON(target)
	if err != nil {
		// Classified rather than assumed: a body that tripped the size cap is
		// a 413, not a 400.
		handleError(c, middleware.BindError(err))
		return false
	}
	return true
}

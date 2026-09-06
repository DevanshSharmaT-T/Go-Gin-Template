// File: internal/modules/roles/api/role_handler.go

package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/service"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/middleware"
)

// RoleHandler is the transport layer for roles and permissions.
type RoleHandler struct {
	roles *service.RoleService
}

// NewRoleHandler builds the handler.
func NewRoleHandler(roles *service.RoleService) *RoleHandler {
	return &RoleHandler{roles: roles}
}

// List returns every role.
func (h *RoleHandler) List(c *gin.Context) {
	var result *service.RoleListResponseDTO
	var err error
	result, err = h.roles.List(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetByID returns one role and its permissions.
func (h *RoleHandler) GetByID(c *gin.Context) {
	var id int64
	var err error
	id, err = strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		handleError(c, errors.NewBadRequestError("that is not a valid role id", err))
		return
	}

	var result *service.RoleResponseDTO
	result, err = h.roles.GetByID(c.Request.Context(), id)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListPermissions returns every permission that can be granted, which is what a
// client needs to render the grant editor.
func (h *RoleHandler) ListPermissions(c *gin.Context) {
	var result *service.PermissionListResponseDTO
	var err error
	result, err = h.roles.ListPermissions(c.Request.Context())
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdatePermissions replaces a role's permissions.
//
// It is a PUT because the body is the complete new set, not a patch.
func (h *RoleHandler) UpdatePermissions(c *gin.Context) {
	var id int64
	var err error
	id, err = strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		handleError(c, errors.NewBadRequestError("that is not a valid role id", err))
		return
	}

	var req service.UpdatePermissionsRequestDTO
	if !bindJSON(c, &req) {
		return
	}

	var result *service.RoleResponseDTO
	result, err = h.roles.UpdatePermissions(c.Request.Context(), id, &req)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// handleError asks the error for its status rather than choosing one.
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

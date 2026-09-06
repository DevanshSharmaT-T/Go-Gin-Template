// File: internal/modules/messages/api/notification_handler.go

package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/service"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/middleware"
)

// NotificationHandler is the transport layer for notifications and the
// outbound-mail log.
type NotificationHandler struct {
	notifications *service.NotificationService
}

// NewNotificationHandler builds the handler.
func NewNotificationHandler(notifications *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{notifications: notifications}
}

// List returns the caller's own notifications.
//
// There is no route that lists somebody else's. The identity comes from the
// token, so the only account reachable here is the caller's — which is why
// these routes need no permission beyond being signed in.
func (h *NotificationHandler) List(c *gin.Context) {
	var userID uuid.UUID
	var ok bool
	userID, ok = middleware.UserIDFrom(c)
	if !ok {
		handleError(c, errors.NewUnauthorizedError("authentication required", nil))
		return
	}

	var result *service.NotificationListResponseDTO
	var err error
	result, err = h.notifications.List(c.Request.Context(), userID, pageFromQuery(c))
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UnreadCount returns the badge number.
func (h *NotificationHandler) UnreadCount(c *gin.Context) {
	var userID uuid.UUID
	var ok bool
	userID, ok = middleware.UserIDFrom(c)
	if !ok {
		handleError(c, errors.NewUnauthorizedError("authentication required", nil))
		return
	}

	var result *service.UnreadCountResponseDTO
	var err error
	result, err = h.notifications.UnreadCount(c.Request.Context(), userID)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// MarkRead marks one of the caller's notifications read.
func (h *NotificationHandler) MarkRead(c *gin.Context) {
	var userID uuid.UUID
	var ok bool
	userID, ok = middleware.UserIDFrom(c)
	if !ok {
		handleError(c, errors.NewUnauthorizedError("authentication required", nil))
		return
	}

	var id uuid.UUID
	var err error
	id, err = uuid.Parse(c.Param("id"))
	if err != nil {
		handleError(c, errors.NewBadRequestError("that is not a valid notification id", err))
		return
	}

	err = h.notifications.MarkRead(c.Request.Context(), userID, id)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "notification marked as read"})
}

// MarkAllRead clears the caller's badge.
func (h *NotificationHandler) MarkAllRead(c *gin.Context) {
	var userID uuid.UUID
	var ok bool
	userID, ok = middleware.UserIDFrom(c)
	if !ok {
		handleError(c, errors.NewUnauthorizedError("authentication required", nil))
		return
	}

	var updated int64
	var err error
	updated, err = h.notifications.MarkAllRead(c.Request.Context(), userID)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"marked_read": updated})
}

// Send raises a notification for another account. Permission-gated.
func (h *NotificationHandler) Send(c *gin.Context) {
	var req service.SendNotificationRequestDTO
	if !bindJSON(c, &req) {
		return
	}

	var result *service.NotificationResponseDTO
	var err error
	result, err = h.notifications.Send(c.Request.Context(), &req)
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListOutboundMail returns the delivery log. Permission-gated: it lists every
// address the system has sent to.
func (h *NotificationHandler) ListOutboundMail(c *gin.Context) {
	var result *service.OutboundMailListResponseDTO
	var err error
	result, err = h.notifications.ListOutboundMail(c.Request.Context(), pageFromQuery(c))
	if err != nil {
		handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// pageFromQuery reads limit and offset, falling back to the defaults rather
// than failing on an unparseable value.
func pageFromQuery(c *gin.Context) domain.Page {
	var page domain.Page
	page.Limit, _ = strconv.Atoi(c.DefaultQuery("limit", "0"))
	page.Offset, _ = strconv.Atoi(c.DefaultQuery("offset", "0"))
	return page.Normalize()
}

// handleError asks the error for its status.
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

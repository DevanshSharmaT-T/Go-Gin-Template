// File: internal/modules/auth/api/auth_handler.go

package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/service"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/middleware"
)

// AuthHandler is the transport layer for the credential endpoints.
//
// Every method has the same three steps: bind, delegate, render. A handler
// containing an `if` about business state belongs in the service, and one
// containing an `if` about a status code is a bug — that decision is made once,
// by the error's classification.
type AuthHandler struct {
	auth *service.AuthService
}

// NewAuthHandler builds the handler.
func NewAuthHandler(auth *service.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

// Register creates an account and sends a verification link.
func (h *AuthHandler) Register(c *gin.Context) {
	var req service.RegisterRequestDTO
	if !bindJSON(c, &req) {
		return
	}

	var result *service.RegisteredDTO
	var err error
	result, err = h.auth.Register(c.Request.Context(), &req)
	if err != nil {
		HandleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, result)
}

// Login exchanges credentials for an access token.
func (h *AuthHandler) Login(c *gin.Context) {
	var req service.LoginRequestDTO
	if !bindJSON(c, &req) {
		return
	}

	var result *service.AuthResponseDTO
	var err error
	result, err = h.auth.Login(c.Request.Context(), &req)
	if err != nil {
		HandleError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// VerifyEmail confirms an address from a token.
//
// It accepts the token in the body or as a `token` query parameter, because the
// link in an email lands on a client page that may forward either.
func (h *AuthHandler) VerifyEmail(c *gin.Context) {
	var req service.VerifyEmailRequestDTO

	var queryToken string = c.Query("token")
	if queryToken != "" {
		req.Token = queryToken
	} else if !bindJSON(c, &req) {
		return
	}

	var result *service.MessageResponseDTO
	var err error
	result, err = h.auth.VerifyEmail(c.Request.Context(), &req)
	if err != nil {
		HandleError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// ForgotPassword starts a password reset.
func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req service.ForgotPasswordRequestDTO
	if !bindJSON(c, &req) {
		return
	}

	var result *service.MessageResponseDTO
	var err error
	result, err = h.auth.RequestPasswordReset(c.Request.Context(), &req)
	if err != nil {
		HandleError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// ResetPassword completes one.
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req service.ResetPasswordRequestDTO
	if !bindJSON(c, &req) {
		return
	}

	var result *service.MessageResponseDTO
	var err error
	result, err = h.auth.ResetPassword(c.Request.Context(), &req)
	if err != nil {
		HandleError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// HandleError is the single place a status code is chosen, and it does not
// choose one: it asks the error.
//
// An unclassified error arriving here is a bug — some layer failed to classify
// — and the safe reading of a bug is that it is our fault, so errors.From
// makes it a 500 with the original kept as the cause for the log.
func HandleError(c *gin.Context, err error) {
	// The request ID is attached here so every error response carries it,
	// whichever layer produced the error — it is what ties a support report to
	// a line in the log.
	var appErr *errors.AppError = errors.From(err).
		WithRequestID(middleware.RequestIDFrom(c))
	c.JSON(appErr.ToHTTPStatus(), appErr.Response())
}

// bindJSON decodes the body, reporting whether it succeeded.
//
// A binding failure is the caller's mistake, so it is a 400 and not a 500. The
// decoder's message is passed through as a field detail rather than as the
// error text: it names the offending field, which is genuinely useful, but it
// also names Go types, which is not something to put in the top-level message.
func bindJSON(c *gin.Context, target any) bool {
	var err error = c.ShouldBindJSON(target)
	if err != nil {
		// Classified rather than assumed: a body that tripped the size cap is
		// a 413, not a 400.
		HandleError(c, middleware.BindError(err))
		return false
	}
	return true
}

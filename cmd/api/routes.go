// File: cmd/api/routes.go

package main

import (
	"net/http"

	"github.com/gin-gonic/gin"

	authapi "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/api"
	userapi "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/api"
)

// registerRoutes is the composition root for the HTTP surface, and it is the
// fx.Invoke that forces the whole graph to be built.
//
// **fx builds lazily: anything not reachable from this parameter list is never
// constructed.** A handler that is provided but not injected here does not
// exist at runtime — its endpoints return 404 while the code compiles, passes
// review and looks entirely present. Adding a module means adding its handler
// to these parameters, not just providing it.
//
// The routes are in three tiers, and the tier a route sits in *is* its access
// control:
//
//	public          no token required
//	authenticated   r.Group("/api") + jwtMiddleware
//	permission      the above + middleware.Authorize(slug, level)
//
// The third tier arrives with the RBAC phase. Until then the administrative
// routes below are authenticated but not permission-gated, which is called out
// where they are registered rather than left to be discovered.
func registerRoutes(
	engine *gin.Engine,
	jwtMiddleware gin.HandlerFunc,
	authHandler *authapi.AuthHandler,
	userHandler *userapi.UserHandler,
) {
	// --- Public ------------------------------------------------------------
	//
	// Everything reachable without a token. Each of these is a place an
	// unauthenticated caller can spend our resources, which is why the auth
	// routes get their own, tighter rate-limit bucket when that lands.

	engine.GET("/healthz", func(c *gin.Context) {
		// A liveness probe with no dependencies: it answers if the process is
		// running. Readiness, which checks the database, arrives with the
		// health module.
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	var auth *gin.RouterGroup = engine.Group("/api/auth")
	{
		auth.POST("/register", authHandler.Register)
		auth.POST("/login", authHandler.Login)
		auth.POST("/verify-email", authHandler.VerifyEmail)
		auth.GET("/verify-email", authHandler.VerifyEmail)
		auth.POST("/forgot-password", authHandler.ForgotPassword)
		auth.POST("/reset-password", authHandler.ResetPassword)
	}

	// --- Authenticated -----------------------------------------------------

	var api *gin.RouterGroup = engine.Group("/api", jwtMiddleware)
	{
		var users *gin.RouterGroup = api.Group("/users")
		{
			// One's own account. No permission needed beyond being signed in:
			// the identity comes from the token, so a caller can only ever
			// reach their own row.
			users.GET("/me", userHandler.Me)
			users.PATCH("/me", userHandler.UpdateMe)
			users.POST("/me/password", userHandler.ChangePassword)

			// Administrative. These read and modify *other* people's accounts
			// and belong in the permission-gated tier:
			//
			//	users.GET("", middleware.Authorize("users:list", RoleManager), userHandler.List)
			//
			// The RBAC phase adds that gate. Until it does they are reachable
			// by any authenticated caller, which is why this template is not
			// deployable before that phase lands.
			users.GET("", userHandler.List)
			users.GET("/:id", userHandler.GetByID)
			users.PATCH("/:id/status", userHandler.UpdateStatus)
		}
	}
}

// File: cmd/api/routes.go

package main

import (
	"net/http"

	"github.com/gin-gonic/gin"

	authapi "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/api"
	messageapi "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/api"
	roleapi "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/api"
	roledomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	userapi "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/api"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/middleware"
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
// **The slugs below are constants, not string literals.** A route gated on a
// slug the seeder never inserted fails closed — it silently removes access
// rather than granting it, and it survives review because nothing about it
// looks wrong. Naming the same constant the catalogue seeds makes a typo a
// compile error instead.
func registerRoutes(
	engine *gin.Engine,
	jwtMiddleware gin.HandlerFunc,
	authHandler *authapi.AuthHandler,
	userHandler *userapi.UserHandler,
	roleHandler *roleapi.RoleHandler,
	notificationHandler *messageapi.NotificationHandler,
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

			// --- Permission-gated -------------------------------------
			//
			// These read and modify *other* people's accounts. Each needs both
			// a permission and a seniority level: the level asks "should this
			// caller be anywhere near this?", the slug asks "has this caller
			// been granted this specific action?".
			users.GET("",
				middleware.Authorize(roledomain.PermUsersList, roledomain.LevelManager),
				userHandler.List)
			users.GET("/:id",
				middleware.Authorize(roledomain.PermUsersRead, roledomain.LevelManager),
				userHandler.GetByID)

			// Suspending an account is an administrator's job, not a manager's,
			// so it sits a level higher as well as behind its own permission.
			users.PATCH("/:id/status",
				middleware.Authorize(roledomain.PermUsersManage, roledomain.LevelAdmin),
				userHandler.UpdateStatus)
		}

		var roleGroup *gin.RouterGroup = api.Group("/roles")
		{
			roleGroup.GET("",
				middleware.Authorize(roledomain.PermRolesList, roledomain.LevelManager),
				roleHandler.List)
			roleGroup.GET("/permissions",
				middleware.Authorize(roledomain.PermRolesRead, roledomain.LevelManager),
				roleHandler.ListPermissions)
			roleGroup.GET("/:id",
				middleware.Authorize(roledomain.PermRolesRead, roledomain.LevelManager),
				roleHandler.GetByID)

			// Changing what a role grants revokes every token that role has
			// outstanding, so it is the most consequential route here.
			roleGroup.PUT("/:id/permissions",
				middleware.Authorize(roledomain.PermRolesManage, roledomain.LevelAdmin),
				roleHandler.UpdatePermissions)
		}

		var notifications *gin.RouterGroup = api.Group("/notifications")
		{
			// Self-scoped: the identity comes from the token, so the only
			// account these can reach is the caller's own. Being signed in is
			// the whole authorization.
			notifications.GET("", notificationHandler.List)
			notifications.GET("/unread-count", notificationHandler.UnreadCount)
			notifications.POST("/read-all", notificationHandler.MarkAllRead)
			notifications.POST("/:id/read", notificationHandler.MarkRead)

			// Writing to somebody else's account is not.
			notifications.POST("",
				middleware.Authorize(roledomain.PermNotificationsSend, roledomain.LevelAdmin),
				notificationHandler.Send)
		}

		// The delivery log lists every address the system has sent to, so it
		// is administrator-only.
		api.GET("/mail",
			middleware.Authorize(roledomain.PermMailList, roledomain.LevelAdmin),
			notificationHandler.ListOutboundMail)
	}
}

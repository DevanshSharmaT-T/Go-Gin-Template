// File: cmd/api/routes.go

package main

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"

	authapi "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/api"
	healthapi "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/health/api"
	messageapi "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/api"
	roleapi "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/api"
	roledomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	userapi "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/api"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
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
	global globalMiddleware,
	jwtMiddleware gin.HandlerFunc,
	authHandler *authapi.AuthHandler,
	userHandler *userapi.UserHandler,
	roleHandler *roleapi.RoleHandler,
	notificationHandler *messageapi.NotificationHandler,
	healthHandler *healthapi.HealthHandler,
) {
	// The global chain, in the order documented in docs/ARCHITECTURE.md. The
	// order is load-bearing and is stated once, here:
	//
	//	recovery      outermost, so it covers the other middleware too
	//	request ID    everything after it can log a correlation ID
	//	logger        needs the request ID, and puts a scoped logger on the ctx
	//	CORS          answers preflights before anything else does work
	//	body limit    caps the read before a decoder is handed the body
	//	rate limit    after the cheap rejections, before the expensive work
	//	timeout       innermost, so its deadline covers only the handler
	engine.Use(
		gin.HandlerFunc(global.Recovery),
		gin.HandlerFunc(global.RequestID),
		gin.HandlerFunc(global.Logging),
		gin.HandlerFunc(global.CORS),
		gin.HandlerFunc(global.BodyLimit),
		gin.HandlerFunc(global.RateLimit),
		gin.HandlerFunc(global.Timeout),
	)

	// A request for a route that does not exist still gets a classified
	// response rather than Gin's plain-text default.
	engine.NoRoute(func(c *gin.Context) {
		notFound(c)
	})
	engine.NoMethod(func(c *gin.Context) {
		methodNotAllowed(c)
	})

	// --- Public ------------------------------------------------------------
	//
	// Everything reachable without a token. Each of these is a place an
	// unauthenticated caller can spend our resources, which is why the auth
	// routes carry a second, tighter rate-limit bucket on top of the global
	// one: the global limit is sized for a person using the application, and
	// credential stuffing is not that.

	// Probes. Unauthenticated by necessity — an orchestrator holds no
	// credential — and exempt from rate limiting, because a probe that gets a
	// 429 is a probe that failed. See middleware.IsProbePath.
	//
	// Liveness checks nothing and readiness checks the dependencies, which is
	// the split that matters: a database outage should take instances out of
	// rotation, not restart them.
	engine.GET(middleware.PathLiveness, healthHandler.Live)
	engine.GET(middleware.PathReadiness, healthHandler.Ready)
	engine.GET(middleware.PathHealth, healthHandler.Ready)

	var auth *gin.RouterGroup = engine.Group("/api/auth", gin.HandlerFunc(global.AuthRateLimit))
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

// globalMiddleware collects the chain so registerRoutes takes one parameter
// rather than eight.
//
// It is an fx.In struct, so adding a middleware is a field here and a provider
// in the middleware module — not another argument threaded through.
type globalMiddleware struct {
	fx.In

	Recovery      middleware.RecoveryMiddleware
	RequestID     middleware.RequestIDMiddleware
	Logging       middleware.LoggingMiddleware
	CORS          middleware.CORSMiddleware
	BodyLimit     middleware.BodyLimitMiddleware
	RateLimit     middleware.RateLimitMiddleware
	AuthRateLimit middleware.AuthRateLimitMiddleware
	Timeout       middleware.TimeoutMiddleware
}

// notFound renders an unmatched route as a classified error.
func notFound(c *gin.Context) {
	var appErr *errors.AppError = errors.NewNotFoundError("no such endpoint", nil).
		WithRequestID(middleware.RequestIDFrom(c))
	c.JSON(appErr.ToHTTPStatus(), appErr.Response())
}

// methodNotAllowed renders a known path used with the wrong verb.
func methodNotAllowed(c *gin.Context) {
	var appErr *errors.AppError = errors.NewMethodNotAllowedError(
		"that method is not allowed on this endpoint", nil).
		WithRequestID(middleware.RequestIDFrom(c))
	c.JSON(appErr.ToHTTPStatus(), appErr.Response())
}

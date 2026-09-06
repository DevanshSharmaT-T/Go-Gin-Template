// File: internal/shared/middleware/authorize.go

package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// SuperAdminLevel is the seniority that bypasses the permission map.
//
// It is declared here rather than imported from the roles module because the
// shared kernel may not import a module. The roles module's LevelSuperAdmin
// must agree with it, and a test asserts that it does.
const SuperAdminLevel int64 = 1

// Authorize gates a route on a permission slug and a seniority level.
//
//	users.GET("/:id", middleware.Authorize(domain.PermUsersRead, domain.LevelManager), h.GetByID)
//
// It must be registered *after* JWTMiddleware, which is what parses the claims
// it reads. On a route with no JWTMiddleware in front of it there are no claims,
// and this refuses rather than falling through — a gate that opens when it
// cannot find the thing it checks is not a gate.
//
// **Both conditions must hold**, and they answer different questions. The level
// is "is this caller senior enough to be anywhere near this?"; the slug is "has
// this caller specifically been granted this action?". A manager who has not
// been granted `users:manage` is refused even though the level would allow it,
// and a user granted `users:manage` by mistake is still refused if the route
// requires manager seniority.
//
// **The comparison is `userLevel <= requiredLevel`, and lower is more senior.**
// Getting the direction wrong here inverts the whole model — it would grant
// every route to the least privileged role — which is why it is written once,
// in this function, rather than at each call site.
func Authorize(slug string, requiredLevel int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		var claims *Claims
		var ok bool
		claims, ok = ClaimsFrom(c)
		if !ok {
			abort(c, errors.NewUnauthorizedError("authentication required", nil))
			return
		}

		// The break-glass. A misconfigured permission table must not lock
		// everyone out of the system that fixes permission tables. It is also,
		// unavoidably, a role that can do anything.
		if claims.HierarchyLevel <= SuperAdminLevel {
			c.Next()
			return
		}

		if claims.HierarchyLevel > requiredLevel {
			abort(c, errors.NewForbiddenError("you do not have access to this resource", nil))
			return
		}

		if !claims.HasPermission(slug) {
			abort(c, errors.NewForbiddenError("you do not have access to this resource", nil))
			return
		}

		c.Next()
	}
}

// RequireLevel gates a route on seniority alone.
//
// Use it where the action has no meaningful slug of its own — an area of the
// API reserved for administrators, say. Prefer Authorize: a permission that can
// be granted and revoked is more useful than one baked into a level.
func RequireLevel(requiredLevel int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		var claims *Claims
		var ok bool
		claims, ok = ClaimsFrom(c)
		if !ok {
			abort(c, errors.NewUnauthorizedError("authentication required", nil))
			return
		}

		if claims.HierarchyLevel > requiredLevel {
			abort(c, errors.NewForbiddenError("you do not have access to this resource", nil))
			return
		}

		c.Next()
	}
}

// File: internal/shared/middleware/claims.go

// Package middleware holds the cross-cutting Gin handlers: authentication now,
// and authorization, CORS, rate limiting, request IDs, timeouts and recovery as
// those phases land.
//
// The JWT claim type lives here rather than in the auth module because both
// ends need it — the auth service signs it, this package verifies it, and the
// authorization middleware reads it — and the shared kernel may not import a
// module. Everything else about authentication (who may log in, what a password
// is worth) stays in the auth module, where it belongs.
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims is the payload of an access token.
//
// **Permissions travel in the token.** That is the design decision the whole
// authorization model rests on: a permission check is a map lookup on a struct
// the middleware has already parsed, with no database round trip on the hot
// path. The cost is that the token is a snapshot, which is what PermVersion
// exists to bound.
//
// Nothing secret goes in here. A JWT is signed, not encrypted, and anyone
// holding one can read every field — so no email address, no name, and
// certainly no hash.
type Claims struct {
	UserID uuid.UUID `json:"user_id"`
	RoleID int64     `json:"role_id"`

	// HierarchyLevel is the seniority of the user's role. Lower is more
	// senior; see docs/ARCHITECTURE.md.
	HierarchyLevel int64 `json:"hierarchy_level"`

	// Permissions is the flattened `resource:action` set the role grants.
	Permissions map[string]bool `json:"permissions"`

	// PermVersion is the role's permission version at the moment the token was
	// issued. A mismatch against the current version means the role's
	// permissions changed underneath this token, and it is refused — which is
	// how a permission change revokes outstanding tokens immediately.
	PermVersion int64 `json:"perm_version"`

	jwt.RegisteredClaims
}

// HasPermission reports whether the token grants a slug.
func (c *Claims) HasPermission(slug string) bool {
	if c == nil {
		return false
	}
	return c.Permissions[slug]
}

// Context keys set by JWTMiddleware.
//
// These four names and their types are documented in docs/ARCHITECTURE.md and
// asserted by downstream code, so they are part of the contract. Prefer the
// typed accessors below to a raw c.Get: a type assertion spelled out at each
// call site is a panic waiting for the day one of these changes.
const (
	ContextUserID         = "user_id"
	ContextRoleID         = "role_id"
	ContextHierarchyLevel = "hierarchy_level"
	ContextPermissions    = "permissions"
)

// setClaims stores the parsed claims and the four documented context keys.
func setClaims(c *gin.Context, claims *Claims) {
	c.Set(claimsKeyName, claims)
	c.Set(ContextUserID, claims.UserID)
	c.Set(ContextRoleID, claims.RoleID)
	c.Set(ContextHierarchyLevel, claims.HierarchyLevel)
	c.Set(ContextPermissions, claims.Permissions)
}

// claimsKeyName is the Gin key holding the whole Claims struct. Gin's context
// is keyed by string rather than by an unexported type, so the name is
// prefixed to keep it clear of anything a handler might set.
const claimsKeyName = "__auth_claims"

// ClaimsFrom returns the parsed claims for the request, and whether the request
// was authenticated at all.
//
// A handler behind JWTMiddleware can rely on the second value being true. One
// on a public route cannot, which is exactly why this returns a boolean rather
// than a possibly-nil pointer that reads fine until it does not.
func ClaimsFrom(c *gin.Context) (*Claims, bool) {
	var value any
	var exists bool
	value, exists = c.Get(claimsKeyName)
	if !exists {
		return nil, false
	}

	var claims *Claims
	var ok bool
	claims, ok = value.(*Claims)
	if !ok {
		return nil, false
	}
	return claims, true
}

// UserIDFrom returns the authenticated user's ID.
func UserIDFrom(c *gin.Context) (uuid.UUID, bool) {
	var claims *Claims
	var ok bool
	claims, ok = ClaimsFrom(c)
	if !ok {
		return uuid.Nil, false
	}
	return claims.UserID, true
}

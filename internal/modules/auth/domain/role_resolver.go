// File: internal/modules/auth/domain/role_resolver.go

package domain

import "context"

// RoleClaims is what an access token needs to know about the signed-in user's
// role: how senior it is, what it grants, and which version of that grant.
type RoleClaims struct {
	RoleID         int64
	HierarchyLevel int64
	Permissions    map[string]bool
	PermVersion    int64
}

// RoleResolver turns a role ID into the claims a token carries.
//
// It is a port because the thing that answers it — the roles module and its
// in-memory permission registry — arrives in the RBAC phase, and auth must not
// wait for it. Login already needs to put a hierarchy level and a permission
// map into the token, and the shape of that token is part of the wire contract;
// only the values behind it are still to come.
type RoleResolver interface {
	Resolve(ctx context.Context, roleID int64) (*RoleClaims, error)
}

// UnprivilegedHierarchyLevel is the least senior level in the documented
// hierarchy, matching the seeded USER role.
//
// It is declared here, in the port's own package, so a test can build claims
// for an ordinary account without importing the roles module. The roles
// catalogue's LevelUser must agree with it, and a test asserts that it does.
const UnprivilegedHierarchyLevel int64 = 100

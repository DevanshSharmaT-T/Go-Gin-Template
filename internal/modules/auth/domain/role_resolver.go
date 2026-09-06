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

// UnprivilegedHierarchyLevel is the seniority the fallback resolver reports.
//
// It matches the seeded USER role. 100 is the least senior level in the
// documented hierarchy, and the comparison is "lower is more senior", so a
// token minted with it passes no level gate that matters.
const UnprivilegedHierarchyLevel int64 = 100

// FallbackRoleResolver grants nothing.
//
// **This is a placeholder, and it fails closed on purpose.** Until the roles
// module exists there is no permission table to read, so it reports the least
// senior level and an empty permission set — every token it contributes to is
// authenticated but unprivileged. The alternative, inventing permissions so
// that routes appear to work, would mean the RBAC phase *removes* access that
// people had come to rely on, which is the worse way round to discover a
// mistake.
type FallbackRoleResolver struct{}

// NewFallbackRoleResolver builds the placeholder resolver.
func NewFallbackRoleResolver() RoleResolver {
	return &FallbackRoleResolver{}
}

// Resolve reports the role as unprivileged.
func (r *FallbackRoleResolver) Resolve(_ context.Context, roleID int64) (*RoleClaims, error) {
	return &RoleClaims{
		RoleID:         roleID,
		HierarchyLevel: UnprivilegedHierarchyLevel,
		Permissions:    map[string]bool{},
		PermVersion:    0,
	}, nil
}

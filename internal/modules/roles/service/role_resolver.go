// File: internal/modules/roles/service/role_resolver.go

package service

import (
	"context"

	authdomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// RegistryRoleResolver fills a token's role claims from the registry. It
// replaces the placeholder resolver the auth phase shipped.
//
// This module implements the auth module's port rather than the other way
// round, because this is the module that owns the data. Depending on another
// module's domain package is the sanctioned direction; its api and infra are
// not.
type RegistryRoleResolver struct {
	registry *domain.PermissionRegistry
}

// NewRegistryRoleResolver builds the resolver, declaring the port as its return
// type so fx binds it there.
func NewRegistryRoleResolver() authdomain.RoleResolver {
	return &RegistryRoleResolver{registry: domain.Registry}
}

// Resolve reads the role's level, version and permissions out of memory.
//
// Login is the one moment the whole permission set is copied into a token, so
// it is worth it being a memory read: the alternative is a join on every sign-in
// to produce something that then does not change for the token's lifetime.
//
// An unknown role is an error rather than an empty grant. It means the account
// references a role that no longer exists, and issuing a valid token with no
// permissions would turn that into a confusing 403 on every route instead of a
// clear failure at the point the problem actually is.
func (r *RegistryRoleResolver) Resolve(_ context.Context, roleID int64) (*authdomain.RoleClaims, error) {
	var level int64
	var known bool
	level, known = r.registry.Level(roleID)
	if !known {
		return nil, errors.NewInternalError(
			"the account references a role that does not exist", nil)
	}

	return &authdomain.RoleClaims{
		RoleID:         roleID,
		HierarchyLevel: level,
		Permissions:    r.registry.Permissions(roleID),
		PermVersion:    r.registry.Version(roleID),
	}, nil
}

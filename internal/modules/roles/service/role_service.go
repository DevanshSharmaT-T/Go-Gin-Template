// File: internal/modules/roles/service/role_service.go

package service

import (
	"context"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// RoleService reads roles and changes what they grant.
type RoleService struct {
	roles    domain.RoleRepository
	registry *domain.PermissionRegistry
}

// NewRoleService builds the service.
func NewRoleService(roles domain.RoleRepository) *RoleService {
	return &RoleService{roles: roles, registry: domain.Registry}
}

// List returns every role with its permissions.
func (s *RoleService) List(ctx context.Context) (*RoleListResponseDTO, error) {
	var roles []*domain.Role
	var err error
	roles, err = s.roles.List(ctx)
	if err != nil {
		return nil, err
	}

	var response *RoleListResponseDTO = &RoleListResponseDTO{
		Roles: make([]*RoleResponseDTO, 0, len(roles)),
	}

	var role *domain.Role
	for _, role = range roles {
		// Read from the registry rather than the database: it is the same
		// answer, already in memory, and it is what authorization will actually
		// use — so a discrepancy shows up here rather than as a mysterious 403.
		var slugs []string = slugsOf(s.registry.Permissions(role.ID))
		response.Roles = append(response.Roles, toRoleResponse(role, slugs))
	}
	return response, nil
}

// GetByID returns one role, or a NOT_FOUND AppError.
func (s *RoleService) GetByID(ctx context.Context, id int64) (*RoleResponseDTO, error) {
	var role *domain.Role
	var err error
	role, err = s.roles.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	var slugs []string
	slugs, err = s.roles.PermissionsFor(ctx, id)
	if err != nil {
		return nil, err
	}
	return toRoleResponse(role, slugs), nil
}

// ListPermissions returns every permission that can be granted.
func (s *RoleService) ListPermissions(ctx context.Context) (*PermissionListResponseDTO, error) {
	var permissions []*domain.Permission
	var err error
	permissions, err = s.roles.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}

	var response *PermissionListResponseDTO = &PermissionListResponseDTO{
		Permissions: make([]*PermissionResponseDTO, 0, len(permissions)),
	}

	var permission *domain.Permission
	for _, permission = range permissions {
		response.Permissions = append(response.Permissions, &PermissionResponseDTO{
			Slug:        permission.Slug,
			Description: permission.Description,
		})
	}
	return response, nil
}

// UpdatePermissions replaces a role's permissions and revokes every token the
// role has outstanding.
//
// The revocation is not a separate step anyone can forget: the repository bumps
// the permission version in the same transaction as the grant change, and this
// writes the new version into the registry. From the next request onwards,
// tokens carrying the old version are refused with TOKEN_STALE and their
// holders sign in again.
//
// SUPER_ADMIN is refused. Its access does not come from the permission map —
// level 1 bypasses it — so editing its permissions would appear to do something
// and would in fact do nothing, which is worse than saying no.
func (s *RoleService) UpdatePermissions(
	ctx context.Context,
	id int64,
	req *UpdatePermissionsRequestDTO,
) (*RoleResponseDTO, error) {
	var role *domain.Role
	var err error
	role, err = s.roles.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if role.IsSuperAdmin() {
		return nil, errors.NewValidationError(
			"the super-administrator role bypasses the permission map, so its permissions cannot be changed",
			nil)
	}

	var slugs []string = dedupe(req.Permissions)

	var version int64
	version, err = s.roles.ReplaceGrants(ctx, id, slugs)
	if err != nil {
		return nil, err
	}

	s.registry.SetRole(id, role.HierarchyLevel, version, slugs)
	role.PermVersion = version

	logger.FromContext(ctx).Info().
		Int64("role_id", id).
		Int64("perm_version", version).
		Int("permissions", len(slugs)).
		Msg("role permissions changed; outstanding tokens for this role are now stale")

	return toRoleResponse(role, slugs), nil
}

// WarmUp rebuilds the registry from the database.
//
// It runs at boot, after seeding, and it is the reason authorization can answer
// from memory at all. Reordering it before seeding leaves the registry empty on
// a fresh database, and the first sign-in issues a token with no permissions —
// which fails closed, but looks like a permissions bug rather than an ordering
// one.
func (s *RoleService) WarmUp(ctx context.Context) error {
	var snapshot *domain.Snapshot
	var err error
	snapshot, err = s.roles.Snapshot(ctx)
	if err != nil {
		return err
	}

	s.registry.Replace(*snapshot)

	logger.FromContext(ctx).Info().
		Int("roles", len(snapshot.Roles)).
		Int("grants", len(snapshot.Grants)).
		Int("suspended", len(snapshot.SuspendedUsers)).
		Msg("permission registry warmed up")

	return nil
}

// slugsOf flattens a permission set into a slice.
func slugsOf(permissions map[string]bool) []string {
	var slugs []string = make([]string, 0, len(permissions))
	var slug string
	for slug = range permissions {
		slugs = append(slugs, slug)
	}
	return slugs
}

// dedupe removes repeats, so a request listing a permission twice does not
// attempt to insert the same grant twice and trip the primary key.
func dedupe(values []string) []string {
	var seen map[string]bool = make(map[string]bool, len(values))
	var unique []string = make([]string, 0, len(values))

	var value string
	for _, value = range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return unique
}

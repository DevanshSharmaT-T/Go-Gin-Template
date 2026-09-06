// File: internal/modules/roles/service/role_dto.go

package service

import (
	"time"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
)

// RoleResponseDTO is the public representation of a role.
type RoleResponseDTO struct {
	ID             int64     `json:"id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	HierarchyLevel int64     `json:"hierarchy_level"`
	PermVersion    int64     `json:"perm_version"`
	Permissions    []string  `json:"permissions"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PermissionResponseDTO is one grantable permission.
type PermissionResponseDTO struct {
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

// UpdatePermissionsRequestDTO replaces a role's permissions wholesale.
//
// It is a replace rather than an add/remove pair because a partial update of a
// permission set is ambiguous under concurrency: two administrators each adding
// one permission would, with a merge, both succeed and neither would see the
// other's change. Sending the whole set makes the last write obviously the
// winner.
type UpdatePermissionsRequestDTO struct {
	Permissions []string `json:"permissions"`
}

// RoleListResponseDTO is every role.
type RoleListResponseDTO struct {
	Roles []*RoleResponseDTO `json:"roles"`
}

// PermissionListResponseDTO is every permission the application defines.
type PermissionListResponseDTO struct {
	Permissions []*PermissionResponseDTO `json:"permissions"`
}

// toRoleResponse maps an entity plus its slugs to the public shape.
func toRoleResponse(role *domain.Role, permissions []string) *RoleResponseDTO {
	if role == nil {
		return nil
	}
	if permissions == nil {
		permissions = []string{}
	}
	return &RoleResponseDTO{
		ID:             role.ID,
		Code:           role.Code,
		Name:           role.Name,
		Description:    role.Description,
		HierarchyLevel: role.HierarchyLevel,
		PermVersion:    role.PermVersion,
		Permissions:    permissions,
		CreatedAt:      role.CreatedAt,
		UpdatedAt:      role.UpdatedAt,
	}
}

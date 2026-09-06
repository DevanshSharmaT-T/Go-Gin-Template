// File: internal/modules/roles/domain/role_model.go

package domain

import "time"

// Role is a named seniority level with a set of permissions.
//
// The id is not auto-assigned: a user row stores it, seeders and tests refer to
// it, and a role whose id depended on insertion order could not be named
// anywhere. See the RoleID constants in catalogue.go.
type Role struct {
	ID             int64  `gorm:"primaryKey;autoIncrement:false"`
	Code           string `gorm:"uniqueIndex;not null;type:varchar(50)"`
	Name           string `gorm:"not null;type:varchar(100)"`
	Description    string `gorm:"type:varchar(255)"`
	HierarchyLevel int64  `gorm:"not null;index"`

	// PermVersion is bumped every time this role's permissions change.
	//
	// It travels in every token the role issues, and the middleware compares it
	// against the current value on each request. That comparison is the entire
	// revocation mechanism: change a role's permissions and every token minted
	// before the change stops verifying, without a database round trip and
	// without waiting for the token to expire.
	PermVersion int64 `gorm:"not null;default:1"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// TableName pins the table name.
func (Role) TableName() string { return "roles" }

// IsSuperAdmin reports whether this role bypasses the permission map.
func (r *Role) IsSuperAdmin() bool { return r.HierarchyLevel <= LevelSuperAdmin }

// Permission is one `resource:action` capability.
type Permission struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	Slug        string `gorm:"uniqueIndex;not null;type:varchar(100)"`
	Description string `gorm:"type:varchar(255)"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// TableName pins the table name.
func (Permission) TableName() string { return "permissions" }

// RolePermission is the grant join.
//
// It is an explicit model rather than a GORM many2many association, because
// every read of it in this application is the warm-up query — one join,
// returning every grant in the system — and an explicit table makes that a
// plain query instead of association machinery.
type RolePermission struct {
	RoleID       int64 `gorm:"primaryKey"`
	PermissionID int64 `gorm:"primaryKey"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
}

// TableName pins the table name.
func (RolePermission) TableName() string { return "role_permissions" }

// Grant is one row of the warm-up query: a role and a slug it holds.
type Grant struct {
	RoleID int64
	Slug   string
}

// File: internal/modules/roles/domain/catalogue.go

package domain

// Hierarchy levels. **A lower number is more senior**, the way Unix process
// priority works, and Authorize grants access when the caller's level is less
// than or equal to the level a route requires — "you must be at least this
// senior".
//
// The gaps are deliberate. Inserting a role between Admin and Manager should be
// a new constant, not a renumbering of every existing role, because the level
// travels inside every outstanding token and renumbering would silently change
// what those tokens mean.
const (
	LevelSuperAdmin int64 = 1
	LevelAdmin      int64 = 10
	LevelManager    int64 = 20
	LevelUser       int64 = 100
)

// Seeded role IDs. They are fixed rather than auto-assigned because a user row
// stores the id, and a role whose id depends on insertion order cannot be
// referred to from a seeder, a migration or a test.
const (
	RoleIDSuperAdmin int64 = 1
	RoleIDAdmin      int64 = 2
	RoleIDManager    int64 = 3
	RoleIDUser       int64 = 4
)

// Permission slugs, in `resource:action` form.
//
// **These constants are the whole defence against the failure the documentation
// warns about**: a route gated on a slug the seeder never inserted fails closed,
// silently removing access rather than granting it, and it survives review
// because nothing about it looks wrong. Comparing two hand-typed strings in two
// files is exactly the kind of check people are bad at.
//
// So there are not two strings. The route and the seeder both name the same
// constant, and [Catalogue] below is what the seeder inserts. A typo is a
// compile error; a slug that exists but is never granted is caught by the test
// that walks the catalogue.
const (
	PermUsersList   = "users:list"
	PermUsersRead   = "users:read"
	PermUsersManage = "users:manage"

	PermRolesList   = "roles:list"
	PermRolesRead   = "roles:read"
	PermRolesManage = "roles:manage"

	PermNotificationsSend = "notifications:send"
	PermMailList          = "mail:list"
)

// PermissionDefinition is one entry in the catalogue: the slug, and a
// description an administrator sees when granting it.
type PermissionDefinition struct {
	Slug        string
	Description string
}

// Catalogue is every permission the application defines.
//
// The seeder inserts exactly this list, so adding a permission is one entry
// here plus its use on a route. Removing one is deliberate work: the slug stays
// in the database until a migration drops it, because deleting a permission row
// revokes it from every role that holds it.
func Catalogue() []PermissionDefinition {
	return []PermissionDefinition{
		{PermUsersList, "List user accounts"},
		{PermUsersRead, "View any user account"},
		{PermUsersManage, "Change a user account's status"},

		{PermRolesList, "List roles"},
		{PermRolesRead, "View a role and its permissions"},
		{PermRolesManage, "Change which permissions a role grants"},

		{PermNotificationsSend, "Send an in-app notification to any account"},
		{PermMailList, "List every email the application has tried to send"},
	}
}

// RoleDefinition is a seeded role and the permissions it starts with.
type RoleDefinition struct {
	ID             int64
	Code           string
	Name           string
	HierarchyLevel int64
	Description    string

	// Grants is the initial permission set. SUPER_ADMIN's is empty on purpose:
	// level 1 bypasses the permission map entirely, so listing permissions for
	// it would suggest the list is what grants the access.
	Grants []string
}

// Roles is the seeded role hierarchy.
//
// These are a starting point, not a fixed model — a template cannot know your
// domain. Rename them, change the levels, add your own; the only things the
// code depends on are that SUPER_ADMIN sits at [LevelSuperAdmin] and that
// [RoleIDUser] is what registration assigns.
func Roles() []RoleDefinition {
	return []RoleDefinition{
		{
			ID: RoleIDSuperAdmin, Code: "SUPER_ADMIN", Name: "Super administrator",
			HierarchyLevel: LevelSuperAdmin,
			Description:    "Unrestricted. Bypasses the permission map; use sparingly.",
			Grants:         nil,
		},
		{
			ID: RoleIDAdmin, Code: "ADMIN", Name: "Administrator",
			HierarchyLevel: LevelAdmin,
			Description:    "Manages users and roles.",
			Grants: []string{
				PermUsersList, PermUsersRead, PermUsersManage,
				PermRolesList, PermRolesRead, PermRolesManage,
				PermNotificationsSend, PermMailList,
			},
		},
		{
			ID: RoleIDManager, Code: "MANAGER", Name: "Manager",
			HierarchyLevel: LevelManager,
			Description:    "Reads user accounts, changes nothing.",
			Grants: []string{
				PermUsersList, PermUsersRead,
				PermRolesList, PermRolesRead,
			},
		},
		{
			ID: RoleIDUser, Code: "USER", Name: "User",
			HierarchyLevel: LevelUser,
			Description:    "The role every registration is assigned. Owns its own account and nothing else.",
			Grants:         nil,
		},
	}
}

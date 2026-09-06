// File: internal/modules/roles/domain/catalogue_test.go

package domain

import (
	"strings"
	"testing"

	authdomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/middleware"
)

// The defect this whole design exists to prevent: a route gated on a slug the
// seeder never inserts. It fails closed — access silently disappears — and it
// survives review because neither file looks wrong on its own.
//
// The constants make a typo a compile error. This closes the other half: a
// constant that exists but was never added to the catalogue, so nothing seeds
// it and every route using it is permanently refused.
func TestCatalogue_ContainsEveryDeclaredSlug(t *testing.T) {
	declared := []string{
		PermUsersList, PermUsersRead, PermUsersManage,
		PermRolesList, PermRolesRead, PermRolesManage,
	}

	seeded := map[string]bool{}
	for _, definition := range Catalogue() {
		seeded[definition.Slug] = true
	}

	for _, slug := range declared {
		if !seeded[slug] {
			t.Errorf("permission %q is declared but not in the catalogue, so it is never "+
				"seeded and every route gated on it will be refused", slug)
		}
	}
}

// The reverse direction: a catalogue entry nothing declares is a slug that can
// be granted through the API and gates nothing, which is a promise the UI makes
// and the server does not keep.
func TestCatalogue_HasNoEntryWithoutAConstant(t *testing.T) {
	declared := map[string]bool{
		PermUsersList: true, PermUsersRead: true, PermUsersManage: true,
		PermRolesList: true, PermRolesRead: true, PermRolesManage: true,
	}

	for _, definition := range Catalogue() {
		if !declared[definition.Slug] {
			t.Errorf("catalogue entry %q has no constant; add one, or remove the entry",
				definition.Slug)
		}
	}
}

// Every grant a seeded role starts with has to be a real permission, or the
// seeder silently drops it and the role boots with less access than intended.
func TestRoles_GrantOnlyCataloguedPermissions(t *testing.T) {
	catalogued := map[string]bool{}
	for _, definition := range Catalogue() {
		catalogued[definition.Slug] = true
	}

	for _, role := range Roles() {
		for _, slug := range role.Grants {
			if !catalogued[slug] {
				t.Errorf("role %s is granted %q, which is not in the catalogue", role.Code, slug)
			}
		}
	}
}

func TestCatalogue_SlugsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}

	for _, definition := range Catalogue() {
		if seen[definition.Slug] {
			t.Errorf("duplicate slug %q", definition.Slug)
		}
		seen[definition.Slug] = true

		// resource:action. The shape is a convention, but a slug that does not
		// follow it is almost always a typo.
		parts := strings.Split(definition.Slug, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			t.Errorf("slug %q is not resource:action", definition.Slug)
		}
		if definition.Slug != strings.ToLower(definition.Slug) {
			t.Errorf("slug %q is not lower case; the comparison is exact", definition.Slug)
		}
		if definition.Description == "" {
			t.Errorf("slug %q has no description, so the grant editor cannot explain it",
				definition.Slug)
		}
	}
}

func TestRoles_AreConsistent(t *testing.T) {
	ids := map[int64]bool{}
	codes := map[string]bool{}
	levels := map[int64]bool{}

	for _, role := range Roles() {
		if ids[role.ID] {
			t.Errorf("duplicate role id %d", role.ID)
		}
		ids[role.ID] = true

		if codes[role.Code] {
			t.Errorf("duplicate role code %q", role.Code)
		}
		codes[role.Code] = true

		if levels[role.HierarchyLevel] {
			t.Errorf("two roles share hierarchy level %d, so neither outranks the other",
				role.HierarchyLevel)
		}
		levels[role.HierarchyLevel] = true

		if role.ID <= 0 {
			t.Errorf("role %s has a non-positive id", role.Code)
		}
	}
}

// SUPER_ADMIN's access comes from the level bypass, not from the permission
// map. Listing permissions for it would suggest the list is what grants them,
// and would leave someone editing the list and wondering why nothing changed.
func TestRoles_SuperAdminHoldsNoExplicitGrants(t *testing.T) {
	for _, role := range Roles() {
		if role.HierarchyLevel == LevelSuperAdmin && len(role.Grants) > 0 {
			t.Fatalf("%s carries explicit grants, but level %d bypasses the permission map",
				role.Code, LevelSuperAdmin)
		}
	}
}

// The registration default has to name a role that is actually seeded, or every
// new account references a role that does not exist and cannot sign in.
func TestRoleIDUser_IsSeeded(t *testing.T) {
	for _, role := range Roles() {
		if role.ID == RoleIDUser {
			if role.HierarchyLevel != LevelUser {
				t.Fatalf("the default role sits at level %d, not LevelUser (%d)",
					role.HierarchyLevel, LevelUser)
			}
			return
		}
	}
	t.Fatalf("RoleIDUser (%d) is not in the seeded roles", RoleIDUser)
}

// The shared kernel may not import this module, so it declares its own copy of
// the super-admin level. The two have to agree, or the bypass fires for the
// wrong role — or for none.
func TestLevels_AgreeWithTheSharedKernel(t *testing.T) {
	if LevelSuperAdmin != middleware.SuperAdminLevel {
		t.Fatalf("LevelSuperAdmin is %d but middleware.SuperAdminLevel is %d; "+
			"the permission-map bypass would fire for the wrong role",
			LevelSuperAdmin, middleware.SuperAdminLevel)
	}
	if LevelUser != authdomain.UnprivilegedHierarchyLevel {
		t.Fatalf("LevelUser is %d but authdomain.UnprivilegedHierarchyLevel is %d",
			LevelUser, authdomain.UnprivilegedHierarchyLevel)
	}
}

// Lower is more senior, and the seeded roles have to be ordered that way for
// the level gates in the routes to mean what they read as.
func TestLevels_AreOrderedMostSeniorFirst(t *testing.T) {
	if LevelSuperAdmin >= LevelAdmin || LevelAdmin >= LevelManager || LevelManager >= LevelUser {
		t.Fatalf("levels are not ordered: super=%d admin=%d manager=%d user=%d",
			LevelSuperAdmin, LevelAdmin, LevelManager, LevelUser)
	}
}

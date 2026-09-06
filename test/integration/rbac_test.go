// File: test/integration/rbac_test.go

//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	rolesdomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	rolesservice "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/service"
	userdomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/domain"
	userservice "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/service"
	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/middleware"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/test/harness"
)

// rbacFixture boots the real graph, including the registry warm-up.
type rbacFixture struct {
	roles *rolesservice.RoleService
	users *userservice.UserService
	repo  userdomain.UserRepository
	guard middleware.TokenGuard
	db    *gorm.DB
}

func newRBACFixture(t *testing.T) *rbacFixture {
	t.Helper()

	f := &rbacFixture{}
	harness.App(t, []any{&f.roles, &f.users, &f.repo, &f.guard, &f.db})

	t.Cleanup(func() {
		if err := f.db.Exec(`DELETE FROM verification_tokens`).Error; err != nil {
			t.Errorf("clearing verification_tokens: %v", err)
		}
		if err := f.db.Exec(`DELETE FROM users`).Error; err != nil {
			t.Errorf("clearing users: %v", err)
		}
		// Put the seeded grants back, so a test that changes them does not
		// leave the next one running against a different permission model.
		if err := f.roles.WarmUp(context.Background()); err != nil {
			t.Errorf("restoring the registry: %v", err)
		}
	})

	return f
}

// The boot sequence's whole point: the registry is populated from what the
// seeders wrote, before any request is served.
func TestRBAC_BootWarmsTheRegistryFromTheSeededRoles(t *testing.T) {
	newRBACFixture(t)

	// Registry is the process singleton the middleware reads.
	registry := rolesdomain.Registry

	if registry.Roles() < len(rolesdomain.Roles()) {
		t.Fatalf("want at least %d roles warmed up, got %d",
			len(rolesdomain.Roles()), registry.Roles())
	}

	for _, definition := range rolesdomain.Roles() {
		level, known := registry.Level(definition.ID)
		if !known {
			t.Errorf("role %s was not warmed up", definition.Code)
			continue
		}
		if level != definition.HierarchyLevel {
			t.Errorf("role %s: want level %d, got %d",
				definition.Code, definition.HierarchyLevel, level)
		}
		if registry.Version(definition.ID) == 0 {
			t.Errorf("role %s warmed up at version 0, which no token can match",
				definition.Code)
		}

		granted := registry.Permissions(definition.ID)
		for _, slug := range definition.Grants {
			if !granted[slug] {
				t.Errorf("role %s was seeded %q but the registry does not have it — "+
					"this is exactly the slug mismatch that fails closed",
					definition.Code, slug)
			}
		}
	}
}

// Every slug a route is gated on must have been seeded, or the gate refuses
// everyone forever. The catalogue makes that true by construction; this checks
// it against a real database.
func TestRBAC_EverySlugInTheCatalogueIsSeeded(t *testing.T) {
	f := newRBACFixture(t)

	result, err := f.roles.ListPermissions(context.Background())
	if err != nil {
		t.Fatalf("listing permissions: %v", err)
	}

	seeded := map[string]bool{}
	for _, permission := range result.Permissions {
		seeded[permission.Slug] = true
	}

	for _, definition := range rolesdomain.Catalogue() {
		if !seeded[definition.Slug] {
			t.Errorf("catalogue slug %q was not seeded into the database", definition.Slug)
		}
	}
}

// Changing a role's permissions bumps its version and updates memory in one
// step, so outstanding tokens are refused from the next request.
func TestRBAC_ChangingPermissionsRevokesOutstandingTokens(t *testing.T) {
	f := newRBACFixture(t)
	ctx := context.Background()

	before := rolesdomain.Registry.Version(rolesdomain.RoleIDManager)

	// A token minted before the change.
	claims := &middleware.Claims{
		UserID:         uuid.New(),
		RoleID:         rolesdomain.RoleIDManager,
		HierarchyLevel: rolesdomain.LevelManager,
		Permissions:    rolesdomain.Registry.Permissions(rolesdomain.RoleIDManager),
		PermVersion:    before,
	}
	if err := f.guard.Check(claims); err != nil {
		t.Fatalf("a current token was refused before any change: %v", err)
	}

	updated, err := f.roles.UpdatePermissions(ctx, rolesdomain.RoleIDManager,
		&rolesservice.UpdatePermissionsRequestDTO{
			Permissions: []string{rolesdomain.PermUsersRead},
		})
	if err != nil {
		t.Fatalf("updating permissions: %v", err)
	}

	if updated.PermVersion <= before {
		t.Fatalf("the permission version was not bumped: %d -> %d", before, updated.PermVersion)
	}

	// The same token, unchanged, is now stale.
	staleErr := f.guard.Check(claims)
	if staleErr == nil {
		t.Fatal("a token issued before the permission change was still accepted")
	}
	if staleErr.Type != apperrors.TypeTokenStale {
		t.Fatalf("want TOKEN_STALE, got %s", staleErr.Type)
	}

	// And the registry reflects the new set, so a fresh token carries it.
	granted := rolesdomain.Registry.Permissions(rolesdomain.RoleIDManager)
	if !granted[rolesdomain.PermUsersRead] {
		t.Fatal("the new permission is not in the registry")
	}
	if granted[rolesdomain.PermUsersList] {
		t.Fatal("a permission that was replaced is still in the registry")
	}
}

// Suspending an account takes effect on the next request, not when the token
// expires.
func TestRBAC_SuspensionTakesEffectImmediately(t *testing.T) {
	f := newRBACFixture(t)
	ctx := context.Background()

	user := &userdomain.User{
		Username: unique("susp"), Email: unique("susp") + "@example.com",
		PasswordHash: "x", FirstName: "S", LastName: "U",
		RoleID: rolesdomain.RoleIDUser, Status: userdomain.UserStatusActive,
	}
	if err := f.repo.Create(ctx, user); err != nil {
		t.Fatalf("creating the account: %v", err)
	}

	claims := &middleware.Claims{
		UserID:         user.ID,
		RoleID:         rolesdomain.RoleIDUser,
		HierarchyLevel: rolesdomain.LevelUser,
		Permissions:    map[string]bool{},
		PermVersion:    rolesdomain.Registry.Version(rolesdomain.RoleIDUser),
	}
	if err := f.guard.Check(claims); err != nil {
		t.Fatalf("an active account's token was refused: %v", err)
	}

	if _, err := f.users.UpdateStatus(ctx, user.ID, userdomain.UserStatusSuspended); err != nil {
		t.Fatalf("suspending: %v", err)
	}

	suspendedErr := f.guard.Check(claims)
	if suspendedErr == nil {
		t.Fatal("a suspended account's existing token was still accepted")
	}
	if suspendedErr.Type != apperrors.TypeUserSuspended {
		t.Fatalf("want USER_SUSPENDED, got %s", suspendedErr.Type)
	}

	// Restoring lifts it, again without a restart.
	if _, err := f.users.UpdateStatus(ctx, user.ID, userdomain.UserStatusActive); err != nil {
		t.Fatalf("restoring: %v", err)
	}
	if err := f.guard.Check(claims); err != nil {
		t.Fatalf("a restored account's token is still refused: %v", err)
	}
}

// A suspension applied before a restart must survive it, or restarting the
// process quietly un-suspends everyone.
func TestRBAC_SuspensionSurvivesAWarmUp(t *testing.T) {
	f := newRBACFixture(t)
	ctx := context.Background()

	user := &userdomain.User{
		Username: unique("persist"), Email: unique("persist") + "@example.com",
		PasswordHash: "x", FirstName: "P", LastName: "S",
		RoleID: rolesdomain.RoleIDUser, Status: userdomain.UserStatusSuspended,
	}
	if err := f.repo.Create(ctx, user); err != nil {
		t.Fatalf("creating the account: %v", err)
	}

	// A warm-up stands in for a restart: it rebuilds the registry from scratch.
	if err := f.roles.WarmUp(ctx); err != nil {
		t.Fatalf("warming up: %v", err)
	}

	if !rolesdomain.Registry.IsSuspended(user.ID) {
		t.Fatal("a suspension stored in the database was lost across a warm-up")
	}
}

// SUPER_ADMIN's access comes from the level bypass. Editing its permission list
// would look like it did something and would in fact do nothing.
func TestRBAC_SuperAdminPermissionsCannotBeEdited(t *testing.T) {
	f := newRBACFixture(t)

	_, err := f.roles.UpdatePermissions(context.Background(), rolesdomain.RoleIDSuperAdmin,
		&rolesservice.UpdatePermissionsRequestDTO{
			Permissions: []string{rolesdomain.PermUsersRead},
		})
	if err == nil {
		t.Fatal("the super-admin role's permissions were editable")
	}
	if !apperrors.IsType(err, apperrors.TypeValidation) {
		t.Fatalf("want VALIDATION, got %v", err)
	}
}

// Granting a slug that does not exist must fail whole rather than applying the
// part it understood.
func TestRBAC_UnknownPermissionIsRefusedWithoutPartialApplication(t *testing.T) {
	f := newRBACFixture(t)
	ctx := context.Background()

	before := rolesdomain.Registry.Permissions(rolesdomain.RoleIDManager)

	_, err := f.roles.UpdatePermissions(ctx, rolesdomain.RoleIDManager,
		&rolesservice.UpdatePermissionsRequestDTO{
			Permissions: []string{rolesdomain.PermUsersRead, "does:notexist"},
		})
	if err == nil {
		t.Fatal("an unknown permission slug was accepted")
	}

	// Nothing was applied: the transaction rolled back.
	after, dbErr := f.roles.GetByID(ctx, rolesdomain.RoleIDManager)
	if dbErr != nil {
		t.Fatalf("reading the role: %v", dbErr)
	}
	if len(after.Permissions) != len(before) {
		t.Fatalf("a failed update changed the grants: %d -> %d", len(before), len(after.Permissions))
	}
}

// An account whose role has been deleted must not receive a usable token.
func TestRBAC_TokenForAnUnknownRoleIsStale(t *testing.T) {
	f := newRBACFixture(t)

	staleErr := f.guard.Check(&middleware.Claims{
		UserID:      uuid.New(),
		RoleID:      99999,
		PermVersion: 1,
	})
	if staleErr == nil {
		t.Fatal("a token naming a role that does not exist was accepted")
	}
	if staleErr.Type != apperrors.TypeTokenStale {
		t.Fatalf("want TOKEN_STALE, got %s", staleErr.Type)
	}
}

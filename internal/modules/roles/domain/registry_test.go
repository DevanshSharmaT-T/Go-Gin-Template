// File: internal/modules/roles/domain/registry_test.go

package domain

import (
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestRegistry_ReplaceLoadsRolesAndGrants(t *testing.T) {
	r := NewPermissionRegistry()

	r.Replace(Snapshot{
		Roles: []Role{
			{ID: 2, HierarchyLevel: LevelAdmin, PermVersion: 3},
			{ID: 4, HierarchyLevel: LevelUser, PermVersion: 1},
		},
		Grants: []Grant{
			{RoleID: 2, Slug: PermUsersList},
			{RoleID: 2, Slug: PermUsersRead},
		},
	})

	if got := r.Version(2); got != 3 {
		t.Fatalf("version: want 3, got %d", got)
	}
	level, known := r.Level(2)
	if !known || level != LevelAdmin {
		t.Fatalf("level: want %d, got %d (known=%v)", LevelAdmin, level, known)
	}

	permissions := r.Permissions(2)
	if !permissions[PermUsersList] || !permissions[PermUsersRead] {
		t.Fatalf("grants did not load: %v", permissions)
	}
	if len(r.Permissions(4)) != 0 {
		t.Fatal("a role with no grants came back with some")
	}
}

// An empty registry grants nothing. A process that has not warmed up must
// refuse gated routes, not allow them.
func TestRegistry_EmptyGrantsNothing(t *testing.T) {
	r := NewPermissionRegistry()

	if len(r.Permissions(1)) != 0 {
		t.Fatal("an empty registry granted a permission")
	}
	// Version 0 matches no issued token, since roles start at 1 — so a token
	// naming an unknown role is stale, and refused.
	if r.Version(1) != 0 {
		t.Fatalf("want version 0 for an unknown role, got %d", r.Version(1))
	}
	if _, known := r.Level(1); known {
		t.Fatal("an empty registry claimed to know a role's level")
	}
}

// Replace swaps rather than merges, so a grant removed in the database
// disappears from memory instead of lingering.
func TestRegistry_ReplaceRemovesWhatIsGone(t *testing.T) {
	r := NewPermissionRegistry()

	r.Replace(Snapshot{
		Roles:  []Role{{ID: 2, HierarchyLevel: LevelAdmin, PermVersion: 1}},
		Grants: []Grant{{RoleID: 2, Slug: PermUsersList}, {RoleID: 2, Slug: PermUsersManage}},
	})
	r.Replace(Snapshot{
		Roles:  []Role{{ID: 2, HierarchyLevel: LevelAdmin, PermVersion: 2}},
		Grants: []Grant{{RoleID: 2, Slug: PermUsersList}},
	})

	permissions := r.Permissions(2)
	if permissions[PermUsersManage] {
		t.Fatal("a revoked permission survived the warm-up")
	}
	if !permissions[PermUsersList] {
		t.Fatal("a retained permission was lost")
	}
}

// A grant naming a role that no longer exists must not invent one.
func TestRegistry_IgnoresGrantsForUnknownRoles(t *testing.T) {
	r := NewPermissionRegistry()

	r.Replace(Snapshot{
		Roles:  []Role{{ID: 2, HierarchyLevel: LevelAdmin, PermVersion: 1}},
		Grants: []Grant{{RoleID: 99, Slug: PermUsersManage}},
	})

	if _, known := r.Level(99); known {
		t.Fatal("a grant created a role that does not exist")
	}
	if len(r.Permissions(99)) != 0 {
		t.Fatal("an unknown role came back with permissions")
	}
}

// The returned map goes into a token. Handing out the registry's own map would
// let one caller mutate the live permission set for every request after it.
func TestRegistry_PermissionsReturnsACopy(t *testing.T) {
	r := NewPermissionRegistry()
	r.Replace(Snapshot{
		Roles:  []Role{{ID: 2, HierarchyLevel: LevelAdmin, PermVersion: 1}},
		Grants: []Grant{{RoleID: 2, Slug: PermUsersList}},
	})

	stolen := r.Permissions(2)
	stolen["users:everything"] = true
	delete(stolen, PermUsersList)

	fresh := r.Permissions(2)
	if fresh["users:everything"] {
		t.Fatal("mutating the returned map added a permission to the registry")
	}
	if !fresh[PermUsersList] {
		t.Fatal("mutating the returned map removed a permission from the registry")
	}
}

func TestRegistry_SetRoleAppliesAChangeImmediately(t *testing.T) {
	r := NewPermissionRegistry()
	r.Replace(Snapshot{Roles: []Role{{ID: 3, HierarchyLevel: LevelManager, PermVersion: 1}}})

	r.SetRole(3, LevelManager, 2, []string{PermRolesList})

	if r.Version(3) != 2 {
		t.Fatalf("version: want 2, got %d", r.Version(3))
	}
	if !r.Permissions(3)[PermRolesList] {
		t.Fatal("the new grant did not take effect")
	}
}

func TestRegistry_SuspensionRoundTrips(t *testing.T) {
	r := NewPermissionRegistry()
	id := uuid.New()

	if r.IsSuspended(id) {
		t.Fatal("a fresh registry reported an account as suspended")
	}

	r.Suspend(id)
	if !r.IsSuspended(id) {
		t.Fatal("suspending had no effect")
	}

	r.Restore(id)
	if r.IsSuspended(id) {
		t.Fatal("restoring had no effect")
	}
}

// A suspension applied before a restart has to survive it, which means the
// warm-up has to load it rather than starting from an empty set.
func TestRegistry_ReplaceLoadsSuspensions(t *testing.T) {
	r := NewPermissionRegistry()
	id := uuid.New()

	r.Replace(Snapshot{SuspendedUsers: []uuid.UUID{id}})

	if !r.IsSuspended(id) {
		t.Fatal("the warm-up did not load the suspended set")
	}
}

// The registry is read by every request goroutine while administrative changes
// write to it. `go test -race` is what makes this test worth anything.
func TestRegistry_IsSafeForConcurrentUse(t *testing.T) {
	r := NewPermissionRegistry()
	r.Replace(Snapshot{Roles: []Role{{ID: 2, HierarchyLevel: LevelAdmin, PermVersion: 1}}})

	id := uuid.New()
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = r.Permissions(2)
				_ = r.Version(2)
				_, _ = r.Level(2)
				_ = r.IsSuspended(id)
				_ = r.Roles()
			}
		}()
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				r.SetRole(2, LevelAdmin, int64(j), []string{PermUsersList})
				r.Suspend(id)
				r.Restore(id)
				r.Replace(Snapshot{
					Roles:  []Role{{ID: 2, HierarchyLevel: LevelAdmin, PermVersion: int64(j)}},
					Grants: []Grant{{RoleID: 2, Slug: PermUsersRead}},
				})
			}
		}(i)
	}

	wg.Wait()
}

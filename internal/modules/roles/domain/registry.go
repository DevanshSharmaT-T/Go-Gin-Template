// File: internal/modules/roles/domain/registry.go

package domain

import (
	"sync"

	"github.com/google/uuid"
)

// Registry is the process-local view of who may do what.
//
// # Why it exists
//
// Authorization reads it on every request, and it answers from memory. The
// alternative — querying the permission table per request — puts a round trip
// in front of every gated route, which is the cost a token carrying its own
// permission map exists to avoid.
//
// # Why it is a package-level singleton
//
// It is deliberately **not** managed by fx. The permission map is process-wide
// state with no meaningful second instance, and threading it through every
// constructor that might one day need it buys nothing. WarmUp rebuilds it from
// the database at boot, which is why the boot sequence has a step for it after
// seeding.
//
// # The limit, stated plainly
//
// **This design is single-instance.** With two replicas behind a load balancer,
// a permission change or a suspension applied on replica A does not reach
// replica B, and requests routed to B keep succeeding with the old rights until
// B restarts. Nothing warns you. The ways out — a short JWT_TTL, a shared store,
// or invalidation events — are compared in docs/ARCHITECTURE.md. This is
// documented rather than solved because the right answer depends on a
// deployment a template cannot know about, but it has to be a decision rather
// than a surprise.
var Registry *PermissionRegistry = NewPermissionRegistry()

// PermissionRegistry holds the permission map, the version of each role's
// grants, and the set of suspended users.
//
// Every method is safe for concurrent use: it is read on every request, from
// every request goroutine, while an administrative change writes to it.
type PermissionRegistry struct {
	mu sync.RWMutex

	permissions map[int64]map[string]bool
	versions    map[int64]int64
	levels      map[int64]int64
	suspended   map[uuid.UUID]struct{}
}

// NewPermissionRegistry builds an empty registry.
//
// An empty registry grants nothing, which is the safe state to start in: a
// process that has not yet warmed up refuses gated routes rather than allowing
// them.
func NewPermissionRegistry() *PermissionRegistry {
	return &PermissionRegistry{
		permissions: map[int64]map[string]bool{},
		versions:    map[int64]int64{},
		levels:      map[int64]int64{},
		suspended:   map[uuid.UUID]struct{}{},
	}
}

// Snapshot is a whole registry state, as read from the database.
type Snapshot struct {
	Roles  []Role
	Grants []Grant

	// SuspendedUsers is the set of accounts that may not authenticate. It is
	// rebuilt at boot along with everything else, because a suspension applied
	// before a restart has to survive it.
	SuspendedUsers []uuid.UUID
}

// Replace swaps the whole registry contents for a new snapshot.
//
// It replaces rather than merges, so a role or a grant deleted in the database
// disappears from memory on the next warm-up instead of lingering. The new maps
// are built before the lock is taken, so readers block only for the swap.
func (r *PermissionRegistry) Replace(snapshot Snapshot) {
	var permissions map[int64]map[string]bool = make(map[int64]map[string]bool, len(snapshot.Roles))
	var versions map[int64]int64 = make(map[int64]int64, len(snapshot.Roles))
	var levels map[int64]int64 = make(map[int64]int64, len(snapshot.Roles))

	var role Role
	for _, role = range snapshot.Roles {
		permissions[role.ID] = map[string]bool{}
		versions[role.ID] = role.PermVersion
		levels[role.ID] = role.HierarchyLevel
	}

	var grant Grant
	for _, grant = range snapshot.Grants {
		if permissions[grant.RoleID] == nil {
			// A grant for a role that no longer exists. Skipping it keeps the
			// registry consistent with the roles table rather than inventing
			// an entry the rest of the system has never heard of.
			continue
		}
		permissions[grant.RoleID][grant.Slug] = true
	}

	var suspended map[uuid.UUID]struct{} = make(map[uuid.UUID]struct{}, len(snapshot.SuspendedUsers))
	var id uuid.UUID
	for _, id = range snapshot.SuspendedUsers {
		suspended[id] = struct{}{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.permissions = permissions
	r.versions = versions
	r.levels = levels
	r.suspended = suspended
}

// Permissions returns a copy of the slugs a role grants.
//
// It is a copy because the result goes into a token, and handing out the
// registry's own map would let any caller mutate the live permission set of
// every request that follows.
func (r *PermissionRegistry) Permissions(roleID int64) map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var granted map[string]bool = make(map[string]bool, len(r.permissions[roleID]))
	var slug string
	for slug = range r.permissions[roleID] {
		granted[slug] = true
	}
	return granted
}

// Version returns a role's current permission version.
//
// An unknown role reports 0, which no issued token carries — roles start at
// version 1 — so a token naming a role that has been deleted is stale, and
// therefore refused. Failing closed is the right direction here.
func (r *PermissionRegistry) Version(roleID int64) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.versions[roleID]
}

// Level returns a role's hierarchy level, and whether the role is known.
func (r *PermissionRegistry) Level(roleID int64) (int64, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var level int64
	var known bool
	level, known = r.levels[roleID]
	return level, known
}

// SetRole records a role's level and version, and replaces its grants.
//
// It is what an administrative permission change calls, so the change takes
// effect on the next request rather than at the next restart.
func (r *PermissionRegistry) SetRole(roleID int64, level int64, version int64, slugs []string) {
	var granted map[string]bool = make(map[string]bool, len(slugs))
	var slug string
	for _, slug = range slugs {
		granted[slug] = true
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.permissions[roleID] = granted
	r.versions[roleID] = version
	r.levels[roleID] = level
}

// IsSuspended reports whether an account is currently suspended. This is the
// check that runs on every authenticated request.
func (r *PermissionRegistry) IsSuspended(userID uuid.UUID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var _, found = r.suspended[userID]
	return found
}

// Suspend blocks an account immediately, without waiting for its token to
// expire.
func (r *PermissionRegistry) Suspend(userID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.suspended[userID] = struct{}{}
}

// Restore lifts a suspension.
func (r *PermissionRegistry) Restore(userID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.suspended, userID)
}

// Roles returns how many roles are loaded. It exists for the boot log line, so
// "the registry warmed up" is a claim with a number attached rather than an
// assertion.
func (r *PermissionRegistry) Roles() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.versions)
}

// File: internal/modules/roles/domain/role_repository.go

package domain

import (
	"context"

	"github.com/google/uuid"
)

// RoleRepository is the persistence port for roles, permissions and grants.
//
// The Find/Get split is the one described in docs/ARCHITECTURE.md: a lookup
// reports absence as (nil, nil), a method addressing one specific row returns
// NOT_FOUND.
type RoleRepository interface {
	// GetByID returns NOT_FOUND when the role does not exist.
	GetByID(ctx context.Context, id int64) (*Role, error)

	// List returns every role, most senior first.
	List(ctx context.Context) ([]*Role, error)

	// PermissionsFor returns the slugs a role currently grants.
	PermissionsFor(ctx context.Context, roleID int64) ([]string, error)

	// ListPermissions returns every permission the application defines.
	ListPermissions(ctx context.Context) ([]*Permission, error)

	// ReplaceGrants sets a role's permissions to exactly slugs and bumps its
	// permission version, in one transaction.
	//
	// The two must be atomic. If the grants were written and the bump were
	// lost, outstanding tokens would keep their old permission map *and* keep
	// verifying — the change would appear to have been applied while every
	// existing session carried on with the old rights.
	//
	// It returns the new version, which the caller writes into the registry.
	ReplaceGrants(ctx context.Context, roleID int64, slugs []string) (int64, error)

	// Snapshot reads the whole registry state in one go, for the boot warm-up.
	Snapshot(ctx context.Context) (*Snapshot, error)

	// SuspendedUserIDs lists the accounts currently suspended, so a restart
	// does not silently lift every suspension.
	SuspendedUserIDs(ctx context.Context) ([]uuid.UUID, error)
}

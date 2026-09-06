// File: internal/modules/roles/service/registry_guard.go

package service

import (
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/middleware"
)

// RegistryGuard is the revocation check that runs on every authenticated
// request. It replaces the placeholder the auth phase shipped.
//
// Both gates read memory and neither touches the database, which is the point:
// an access token is only worth having if checking it costs no round trip, and
// a revocation check that queries per request gives that straight back.
type RegistryGuard struct {
	registry *domain.PermissionRegistry
}

// NewRegistryGuard builds the guard over the process registry.
//
// It returns the middleware.TokenGuard interface, which is what the
// authentication middleware depends on — and what makes this a drop-in for the
// placeholder rather than a change to the middleware.
func NewRegistryGuard() middleware.TokenGuard {
	return &RegistryGuard{registry: domain.Registry}
}

// Check refuses a token whose bearer is suspended or whose permissions have
// changed underneath it.
//
// Suspension is checked first. A suspended account should hear that it is
// suspended, not that its token is stale, and a suspended user whose role also
// changed would otherwise be told to sign in again — which would not work and
// would not explain why.
func (g *RegistryGuard) Check(claims *middleware.Claims) *errors.AppError {
	if g.registry.IsSuspended(claims.UserID) {
		return errors.NewUserSuspendedError("this account has been suspended", nil)
	}

	// A role deleted since the token was issued reports version 0, which no
	// issued token carries, so it lands here and is refused. Failing closed is
	// the right direction.
	if g.registry.Version(claims.RoleID) != claims.PermVersion {
		return errors.NewTokenStaleError(
			"your permissions have changed; sign in again", nil)
	}

	return nil
}

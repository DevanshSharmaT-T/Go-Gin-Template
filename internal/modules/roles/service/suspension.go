// File: internal/modules/roles/service/suspension.go

package service

import (
	"github.com/google/uuid"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	userdomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/domain"
)

// RegistrySuspensions implements the users module's suspension port over the
// permission registry.
//
// It is what turns "the row now says suspended" into "the next request is
// refused". Without it, suspending an account would take effect only when the
// user's current token expired, which for the default 24-hour TTL is not what
// anyone means by suspending an account.
type RegistrySuspensions struct {
	registry *domain.PermissionRegistry
}

// NewRegistrySuspensions builds the adapter, declaring the users module's port
// as its return type.
func NewRegistrySuspensions() userdomain.SuspensionRegistry {
	return &RegistrySuspensions{registry: domain.Registry}
}

// Suspend blocks the account from the next request onwards.
func (s *RegistrySuspensions) Suspend(userID uuid.UUID) {
	s.registry.Suspend(userID)
}

// Restore lifts the block.
func (s *RegistrySuspensions) Restore(userID uuid.UUID) {
	s.registry.Restore(userID)
}

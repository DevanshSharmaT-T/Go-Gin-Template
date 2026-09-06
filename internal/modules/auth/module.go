// File: internal/modules/auth/module.go

package auth

import (
	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/api"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/service"
)

// Module wires the auth module.
//
// It registers no models: auth owns no tables. It borrows the users module's
// repositories, which is also why it has no infra/ directory.
//
// NewFallbackRoleResolver is the placeholder the RBAC phase replaces. When the
// roles module lands, its own provider supplies domain.RoleResolver and this
// line is deleted — two providers of one type is a duplicate-type error at
// startup, which is the failure mode you want for "somebody forgot to remove
// the placeholder".
var Module = fx.Module("auth",
	fx.Provide(
		domain.NewFallbackRoleResolver,
		service.NewAuthService,
		api.NewAuthHandler,
	),
)

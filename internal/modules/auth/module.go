// File: internal/modules/auth/module.go

package auth

import (
	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/api"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/service"
)

// Module wires the auth module.
//
// It registers no models: auth owns no tables. It borrows the users module's
// repositories, which is also why it has no infra/ directory.
//
// domain.RoleResolver is not provided here. The roles module supplies it, from
// the permission registry — auth declares the port it needs and the module that
// owns the data implements it.
var Module = fx.Module("auth",
	fx.Provide(
		service.NewAuthService,
		api.NewAuthHandler,
	),
)

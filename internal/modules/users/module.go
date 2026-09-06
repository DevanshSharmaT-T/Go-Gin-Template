// File: internal/modules/users/module.go

package users

import (
	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/api"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/infra"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/service"
)

// Module wires the users module.
//
// The two `group:"models"` stanzas are what create the tables. Omit one and the
// application still boots, then fails on the first query against a table that
// was never made — which is why they sit next to the repository they belong to
// rather than in a central list somebody has to remember to edit.
var Module = fx.Module("users",
	fx.Provide(
		// Repositories. Each constructor declares the *interface* as its return
		// type; fx binds on that, so returning the concrete type here would
		// break every consumer's resolution at startup.
		infra.NewGormUserRepository,
		infra.NewGormVerificationTokenRepository,

		service.NewUserService,
		api.NewUserHandler,

		fx.Annotate(
			func() any { return &domain.User{} },
			fx.ResultTags(`group:"models"`),
		),
		fx.Annotate(
			func() any { return &domain.VerificationToken{} },
			fx.ResultTags(`group:"models"`),
		),
	),
)

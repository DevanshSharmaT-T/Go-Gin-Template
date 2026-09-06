// File: internal/modules/roles/module.go

package roles

import (
	"context"

	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/api"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/infra"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/seeds"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/service"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database"
)

// Module wires the RBAC module.
//
// It supplies three things nothing else can, and each replaces a placeholder
// the earlier phases shipped:
//
//   - middleware.TokenGuard — the revocation gates. The auth phase's
//     placeholder accepted every valid token.
//   - authdomain.RoleResolver — the permissions that go into a new token. The
//     placeholder granted none.
//   - userdomain.SuspensionRegistry — what makes a suspension take effect now
//     rather than when the token expires.
//
// Providing two constructors for one type is a duplicate-type error at startup,
// so leaving a placeholder behind is a boot failure rather than a silent
// downgrade to it — which is the failure mode you want.
var Module = fx.Module("roles",
	fx.Provide(
		infra.NewGormRoleRepository,
		service.NewRoleService,
		api.NewRoleHandler,

		service.NewRegistryGuard,
		service.NewRegistryRoleResolver,
		service.NewRegistrySuspensions,

		fx.Annotate(
			func() any { return &domain.Role{} },
			fx.ResultTags(`group:"models"`),
		),
		fx.Annotate(
			func() any { return &domain.Permission{} },
			fx.ResultTags(`group:"models"`),
		),
		fx.Annotate(
			func() any { return &domain.RolePermission{} },
			fx.ResultTags(`group:"models"`),
		),

		fx.Annotate(
			seeds.NewRoleSeeder,
			fx.ResultTags(`group:"seeders"`),
		),
		fx.Annotate(
			seeds.NewPermissionSeeder,
			fx.ResultTags(`group:"seeders"`),
		),
	),

	fx.Invoke(registerWarmUp),
)

// registerWarmUp loads the registry at boot.
//
// **The hook order is load-bearing.** fx runs OnStart hooks in the order they
// were appended, and hooks are appended as constructors run, which follows the
// order of the invokes. The database module's invoke comes first in
// internal/app.Modules, so its hook — connect, migrate, seed — is appended
// first and therefore runs first. This one reads what that seeding wrote.
//
// Move this module above database.Module in that list and the registry warms up
// from an empty database: the first sign-in then issues a token with no
// permissions, every gated route returns 403, and nothing says why.
func registerWarmUp(lc fx.Lifecycle, roles *service.RoleService) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return roles.WarmUp(ctx)
		},
	})
}

// Ensure the seeders keep the type the value group collects. A seeder declared
// as a bare func(*gorm.DB) error compiles and never runs.
var _ database.SeedFunc = seeds.NewRoleSeeder()

var _ database.SeedFunc = seeds.NewPermissionSeeder()

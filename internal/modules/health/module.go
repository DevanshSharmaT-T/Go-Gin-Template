// File: internal/modules/health/module.go

package health

import (
	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/health/api"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/health/service"
)

// Module wires the health module.
//
// It registers no models and no seeders: it owns no tables. It is the one
// module with neither a domain nor an infra package, because it reports on
// dependencies other modules own rather than persisting anything of its own.
var Module = fx.Module("health",
	fx.Provide(
		service.NewHealthService,
		api.NewHealthHandler,
	),
)

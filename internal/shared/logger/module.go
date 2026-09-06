// File: internal/shared/logger/module.go

package logger

import (
	"github.com/rs/zerolog"
	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
)

// Module provides the application logger.
var Module = fx.Module("logger",
	fx.Provide(NewFromConfig),
)

// NewFromConfig builds the logger from configuration and installs it as the
// package default.
//
// The SetDefault call is the point. Everything in the shared kernel logs
// through FromContext, which falls back to the default when a context carries
// no request-scoped logger — as it does during boot, before any request
// exists. Without this the whole startup sequence would log at the hardcoded
// info/JSON default instead of what the operator configured.
func NewFromConfig(cfg *config.Config) zerolog.Logger {
	var log zerolog.Logger = New(Options{
		Level:       cfg.Log.Level,
		Format:      cfg.Log.Format,
		AppName:     cfg.App.Name,
		Environment: cfg.App.Env.String(),
	})

	SetDefault(log)
	return log
}

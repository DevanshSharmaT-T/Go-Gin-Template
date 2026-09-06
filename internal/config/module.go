// File: internal/config/module.go

package config

import "go.uber.org/fx"

// Module makes the parsed configuration available to the dependency graph.
//
// It is the first module every entry point and every test harness includes:
// almost everything else takes a *Config parameter, and fx builds it exactly
// once for the whole process.
//
// Load returns (*Config, error), which fx treats as a construction failure. An
// invalid .env therefore stops the application while the graph is still being
// built — before a socket is opened, a connection is made or a migration runs —
// and the error is the full problem list from Load, not the first complaint.
var Module = fx.Module("config",
	fx.Provide(Load),
)

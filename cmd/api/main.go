// File: cmd/api/main.go

// Command api is the service entry point.
//
// It does almost nothing itself: it names the modules that make up the
// application and hands them to fx, which reads their constructors, works out
// the order and builds only what is reachable. The composition root does not
// grow as modules are added — a new module adds one line here and wires itself
// in its own module.go.
package main

import (
	"time"

	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/app"
)

// startTimeout bounds the whole boot sequence: connecting, creating the
// extension, migrating, seeding and binding the listener.
//
// fx's default is 15 seconds, which is comfortable for a warm database and not
// for the first boot against an empty one, where AutoMigrate is creating every
// table. Exceeding it aborts startup with a context deadline, and the migration
// that was halfway through is rolled back — safe, but a confusing way to find
// out the default was too low.
const (
	startTimeout = 2 * time.Minute
	stopTimeout  = 30 * time.Second
)

func main() {
	fx.New(
		fx.StartTimeout(startTimeout),
		fx.StopTimeout(stopTimeout),

		// fx's own diagnostics go through the application logger, so a wiring
		// failure is reported in the same format as everything else.
		fx.WithLogger(fxLogger),

		// Configuration, the shared kernel and every feature module. The list
		// lives in internal/app because the test harness composes the same one.
		app.Modules,

		// The HTTP surface. registerRoutes is the invoke that forces the graph
		// to build — see its doc comment.
		fx.Provide(
			newEngine,
			newHTTPServer,
		),
		fx.Invoke(
			registerRoutes,
			runServer,
		),
	).Run()
}

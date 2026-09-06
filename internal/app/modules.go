// File: internal/app/modules.go

// Package app names the modules that make up the application.
//
// It exists so there is exactly one list. The entry point composes it, and so
// does the test harness — which is what makes "the harness boots the real
// graph" a fact rather than an intention. With the list written out in both
// places, a module added to one and forgotten in the other produces a test
// suite that passes against an application shape nobody deploys.
//
// It deliberately contains no HTTP: the engine, the routes and the server live
// in cmd/api, because a test that wants the graph usually does not want a
// listening socket.
package app

import (
	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/crypt"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/mail"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/middleware"
)

// Modules is the whole application below the transport layer: configuration,
// the shared kernel, and every feature module.
//
// Adding a feature module means adding one line here. Everything else about it
// — its repositories, service, handler and the models it registers for
// migration — is declared in its own module.go.
var Modules = fx.Options(
	// The shared kernel, in dependency order for readability; fx works the
	// order out itself from the constructor signatures.
	config.Module,
	logger.Module,
	database.Module,
	crypt.Module,
	mail.Module,
	middleware.Module,

	// Feature modules.
	users.Module,
	auth.Module,
)

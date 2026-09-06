// File: cmd/api/server.go

package main

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// newEngine builds the Gin engine.
//
// Gin's own Logger and Recovery middleware are deliberately not installed. The
// logger writes unstructured lines to stdout, which would sit alongside this
// application's JSON on stderr and defeat the point of structured logging; a
// request logger and a recovery handler that classify through AppError arrive
// with the middleware phase. Until then the router is bare.
func newEngine(cfg *config.Config) *gin.Engine {
	if !cfg.App.IsDevelopment() {
		gin.SetMode(gin.ReleaseMode)
	}

	var engine *gin.Engine = gin.New()

	// Gin trusts every proxy by default, which means any client can set
	// X-Forwarded-For and choose the IP that rate limiting and audit logging
	// will believe. An empty list trusts nothing; TRUSTED_PROXIES opts in.
	var err error = engine.SetTrustedProxies(cfg.Server.TrustedProxies)
	if err != nil {
		// The values are validated as CIDRs or IPs at startup, so this cannot
		// fail on configuration that passed validation.
		logger.Default().Error().Err(err).Msg("could not apply TRUSTED_PROXIES")
	}

	return engine
}

// newHTTPServer wraps the engine in a net/http server with every timeout set.
//
// Go's http.Server has no default timeouts at all. Without them a handful of
// clients that open a connection and send nothing hold it open indefinitely,
// and the process runs out of file descriptors while looking perfectly healthy.
func newHTTPServer(cfg *config.Config, engine *gin.Engine) *http.Server {
	return &http.Server{
		Addr:              cfg.Server.Address(),
		Handler:           engine,
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.HeaderTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}
}

// runServer starts listening on OnStart and drains on OnStop.
//
// The listener is opened synchronously, inside the start hook, and only then is
// Serve handed to a goroutine. That ordering is what makes "port already in
// use" a startup failure rather than a message logged from a goroutine while
// fx reports a successful boot.
func runServer(lc fx.Lifecycle, cfg *config.Config, server *http.Server) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			var listenConfig net.ListenConfig
			var listener net.Listener
			var err error
			listener, err = listenConfig.Listen(ctx, "tcp", server.Addr)
			if err != nil {
				return err
			}

			logger.FromContext(ctx).Info().
				Str("address", server.Addr).
				Str("env", cfg.App.Env.String()).
				Msg("http server listening")

			go func() {
				var serveErr error = server.Serve(listener)
				// ErrServerClosed is what a graceful shutdown looks like from
				// in here; anything else ended the listener unexpectedly.
				if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
					logger.Default().Error().Err(serveErr).Msg("http server stopped unexpectedly")
				}
			}()

			return nil
		},

		OnStop: func(ctx context.Context) error {
			// Shutdown stops accepting new connections and waits for in-flight
			// requests to finish. The deadline bounds that wait: without one, a
			// single long request holds up the whole shutdown, and the
			// orchestrator's own patience is not unlimited.
			var shutdownCtx context.Context
			var cancel context.CancelFunc
			shutdownCtx, cancel = context.WithTimeout(ctx, cfg.Server.ShutdownTimeout)
			defer cancel()

			logger.FromContext(ctx).Info().Msg("draining in-flight requests")
			return server.Shutdown(shutdownCtx)
		},
	})
}

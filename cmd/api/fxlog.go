// File: cmd/api/fxlog.go

package main

import (
	"github.com/rs/zerolog"
	"go.uber.org/fx/fxevent"
)

// fxLogger routes fx's lifecycle events into the application logger.
//
// The events are noisy — one per constructor, one per hook — so they go out at
// debug level, where LOG_LEVEL=debug turns them on for exactly the situation
// they help with: a graph that will not resolve. Errors are the exception and
// always surface, because "fx could not build the application" is not something
// to discover by raising a log level.
func fxLogger(log zerolog.Logger) fxevent.Logger {
	return &fxeventLogger{log: log.With().Str("component", "fx").Logger()}
}

type fxeventLogger struct {
	log zerolog.Logger
}

// LogEvent renders one fx event.
func (l *fxeventLogger) LogEvent(event fxevent.Event) {
	switch e := event.(type) {
	case *fxevent.OnStartExecuted:
		if e.Err != nil {
			l.log.Error().Err(e.Err).Str("callee", e.FunctionName).Msg("start hook failed")
			return
		}
		l.log.Debug().Str("callee", e.FunctionName).Dur("runtime", e.Runtime).Msg("start hook ran")

	case *fxevent.OnStopExecuted:
		if e.Err != nil {
			l.log.Error().Err(e.Err).Str("callee", e.FunctionName).Msg("stop hook failed")
			return
		}
		l.log.Debug().Str("callee", e.FunctionName).Dur("runtime", e.Runtime).Msg("stop hook ran")

	case *fxevent.Provided:
		if e.Err != nil {
			l.log.Error().Err(e.Err).Msg("could not provide a constructor")
			return
		}
		l.log.Debug().Strs("types", e.OutputTypeNames).Str("constructor", e.ConstructorName).Msg("provided")

	case *fxevent.Invoked:
		if e.Err != nil {
			// This is the one that names the type fx could not resolve.
			l.log.Error().Err(e.Err).Str("function", e.FunctionName).Msg("invoke failed")
			return
		}
		l.log.Debug().Str("function", e.FunctionName).Msg("invoked")

	case *fxevent.Started:
		if e.Err != nil {
			l.log.Error().Err(e.Err).Msg("application failed to start")
			return
		}
		l.log.Info().Msg("application started")

	case *fxevent.Stopped:
		if e.Err != nil {
			l.log.Error().Err(e.Err).Msg("application stopped with errors")
			return
		}
		l.log.Info().Msg("application stopped")

	case *fxevent.Stopping:
		l.log.Info().Str("signal", e.Signal.String()).Msg("shutting down")
	}
}

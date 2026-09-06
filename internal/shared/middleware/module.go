// File: internal/shared/middleware/module.go

package middleware

import (
	"go.uber.org/fx"
)

// Module provides the token codec and the authentication middleware.
//
// JWTMiddleware is provided as a bare gin.HandlerFunc. fx keys providers by
// type, so this is the one and only provider in the graph that may return that
// type — a second one collides with a duplicate-type error at startup. Every
// middleware added later gets a named type:
//
//	type RequestIDMiddleware gin.HandlerFunc
//
// TokenGuard is not provided here. The RBAC module supplies it from the
// permission registry, because the state it reads belongs to that module and
// the shared kernel may not import one.
var Module = fx.Module("middleware",
	fx.Provide(
		NewTokenCodec,
		JWTMiddleware,

		// Every other middleware has a named type, for the reason above. The
		// order they are installed in is fixed in cmd/api, not here — fx works
		// out construction order, not execution order.
		NewRecoveryMiddleware,
		NewRequestIDMiddleware,
		NewLoggingMiddleware,
		NewCORSMiddleware,
		NewBodyLimitMiddleware,
		NewRateLimitMiddleware,
		NewAuthRateLimitMiddleware,
		NewTimeoutMiddleware,
	),
)

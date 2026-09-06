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
// AllowAllGuard is the placeholder revocation check, replaced by the permission
// registry in the RBAC phase. See its doc comment for what it does not do.
var Module = fx.Module("middleware",
	fx.Provide(
		NewTokenCodec,
		JWTMiddleware,
		func() TokenGuard { return AllowAllGuard{} },
	),
)

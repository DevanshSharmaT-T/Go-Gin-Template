// File: internal/shared/middleware/cors.go

package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
)

// CORSMiddleware applies the cross-origin policy.
type CORSMiddleware gin.HandlerFunc

// NewCORSMiddleware builds the middleware from the configured allow-list.
//
// # What makes this worth owning rather than importing
//
// The whole of CORS security is in one decision: which origins get an
// `Access-Control-Allow-Origin` header back. Three properties matter, and each
// is a line below rather than a setting in somebody else's library.
//
//  1. **An untrusted origin is never echoed.** The tempting implementation
//     reflects whatever arrived in the `Origin` header, which is the same as
//     having no policy at all — every site becomes an allowed origin, and with
//     credentials enabled every authenticated endpoint becomes readable by any
//     page the user visits. A request from an origin that is not on the list
//     simply gets no CORS headers, and the browser refuses the response.
//
//  2. **The match is exact.** Not a prefix, not a suffix, not `strings.Contains`.
//     A suffix test on `example.com` accepts `evil-example.com`; a prefix test
//     on `https://app.example.com` accepts `https://app.example.com.evil.net`.
//
//  3. **`Vary: Origin` is always set**, on every response, including the ones
//     that get no CORS headers at all. Without it a shared cache can store the
//     response to an allowed origin and serve it to a disallowed one, which
//     hands out the header the allow-list just refused.
//
// The `*`-with-credentials combination is rejected at startup rather than here;
// see internal/config.
func NewCORSMiddleware(cfg *config.Config) CORSMiddleware {
	var allowMethods string = strings.Join(cfg.CORS.AllowedMethods, ", ")
	var allowHeaders string = strings.Join(cfg.CORS.AllowedHeaders, ", ")
	var exposeHeaders string = strings.Join(cfg.CORS.ExposedHeaders, ", ")
	var maxAge string = strconv.Itoa(int(cfg.CORS.MaxAge.Seconds()))

	return func(c *gin.Context) {
		// Set before anything else, and regardless of the outcome: the response
		// varies by Origin even when the answer is "no headers for you".
		c.Header("Vary", "Origin")

		var origin string = c.GetHeader("Origin")

		// Not a cross-origin request. Same-origin and server-to-server calls
		// send no Origin header and need no CORS headers.
		if origin == "" {
			c.Next()
			return
		}

		if !cfg.CORS.AllowsOrigin(origin) {
			// No headers. The request still runs — CORS is enforced by the
			// browser on the *response*, and refusing to serve here would break
			// non-browser clients, which are not subject to it at all and are
			// not what this protects.
			//
			// A preflight is the exception: there is nothing to run, and
			// answering 204 without the headers would be a confusing way to say
			// no.
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		// Echoed rather than wildcarded, because `*` is invalid with
		// credentials and because the response is already varied by Origin.
		c.Header("Access-Control-Allow-Origin", origin)

		if cfg.CORS.AllowCredentials {
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		if exposeHeaders != "" {
			c.Header("Access-Control-Expose-Headers", exposeHeaders)
		}

		if c.Request.Method == http.MethodOptions {
			c.Header("Access-Control-Allow-Methods", allowMethods)
			c.Header("Access-Control-Allow-Headers", allowHeaders)
			c.Header("Access-Control-Max-Age", maxAge)

			// A preflight is answered here and goes no further. Letting it
			// through would run the rate limiter, the auth middleware and the
			// handler for a request that carries no credentials and expects no
			// body.
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

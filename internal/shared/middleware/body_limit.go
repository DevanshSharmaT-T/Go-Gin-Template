// File: internal/shared/middleware/body_limit.go

package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// BodyLimitMiddleware caps how much a client may send.
type BodyLimitMiddleware gin.HandlerFunc

// NewBodyLimitMiddleware builds the middleware.
//
// An unbounded request body is a memory-exhaustion primitive: a JSON decoder
// asked to parse a body will happily allocate as much as it is given, and one
// client can do that from one connection.
//
// Two checks, because one is not enough. The declared Content-Length is
// rejected outright, which costs nothing and never reads the body. A client
// that lies about it — or uses chunked encoding and declares nothing — is
// caught by MaxBytesReader, which stops the read at the limit and makes the
// decode fail. [BindError] turns that failure into a 413 rather than the 400 a
// malformed body would get.
func NewBodyLimitMiddleware(cfg *config.Config) BodyLimitMiddleware {
	var limit int64 = cfg.Server.MaxRequestBodyBytes

	return func(c *gin.Context) {
		if c.Request.ContentLength > limit {
			abort(c, apperrors.NewPayloadTooLargeError("request body is too large", nil).
				WithRequestID(RequestIDFrom(c)))
			return
		}

		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}

		c.Next()
	}
}

// BindError classifies a request-binding failure.
//
// Handlers call it instead of assuming every bind failure is a 400. A body that
// tripped the size cap is a 413, and the difference matters to a client
// deciding whether to fix its payload or shrink it.
func BindError(err error) *apperrors.AppError {
	if err == nil {
		return nil
	}

	var tooLarge *http.MaxBytesError
	if apperrors.As(err, &tooLarge) {
		return apperrors.NewPayloadTooLargeError("request body is too large", err)
	}

	return apperrors.NewBadRequestError("the request body is not valid", err).
		WithDetail("body", err.Error())
}

// File: internal/shared/middleware/jwt.go

package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// signingMethod is the only algorithm this application accepts.
//
// It is named in one place and enforced with jwt.WithValidMethods on every
// parse. That is what closes the two classic JWT attacks: `alg: none`, where a
// token claims to need no signature, and RS256-to-HS256 confusion, where a
// public key is presented as an HMAC secret. A parser that trusts the token's
// own header to choose the algorithm is asking the attacker what verification
// to perform.
var signingMethod *jwt.SigningMethodHMAC = jwt.SigningMethodHS256

// TokenCodec signs and verifies access tokens.
//
// It lives in the shared kernel because both the auth module (which signs) and
// this package (which verifies) need it, and shared code may not import a
// module.
type TokenCodec struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

// NewTokenCodec builds the codec from configuration.
func NewTokenCodec(cfg *config.Config) *TokenCodec {
	return &TokenCodec{
		secret: []byte(cfg.Auth.JWTSecret),
		issuer: cfg.Auth.JWTIssuer,
		ttl:    cfg.Auth.JWTTTL,
	}
}

// TTL is the configured access-token lifetime, which the login response reports
// to the client as expires_in.
func (t *TokenCodec) TTL() time.Duration {
	return t.ttl
}

// Sign issues an access token carrying claims.
//
// The registered claims — issuer, issued-at, expiry — are set here rather than
// by the caller, so that every token in the system agrees on them and no code
// path can accidentally mint one that never expires.
func (t *TokenCodec) Sign(claims *Claims) (string, time.Time, error) {
	var now time.Time = time.Now().UTC()
	var expiresAt time.Time = now.Add(t.ttl)

	claims.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    t.issuer,
		Subject:   claims.UserID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}

	var token *jwt.Token = jwt.NewWithClaims(signingMethod, claims)

	var signed string
	var err error
	signed, err = token.SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, errors.NewInternalError("could not sign the access token", err)
	}

	return signed, expiresAt, nil
}

// Parse verifies a token and returns its claims.
//
// Every verification option is explicit. WithValidMethods pins the algorithm,
// WithIssuer rejects a token minted by a different service that happens to
// share a secret, and WithExpirationRequired refuses one that simply omits the
// expiry claim — which the library would otherwise treat as "never expires".
func (t *TokenCodec) Parse(tokenString string) (*Claims, error) {
	var claims *Claims = &Claims{}

	var token *jwt.Token
	var err error
	token, err = jwt.ParseWithClaims(
		tokenString,
		claims,
		func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{signingMethod.Alg()}),
		jwt.WithIssuer(t.issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, errors.NewTokenExpiredError("your session has expired; sign in again", err)
		}
		return nil, errors.NewTokenInvalidError("invalid authentication token", err)
	}
	if !token.Valid {
		return nil, errors.NewTokenInvalidError("invalid authentication token", nil)
	}

	return claims, nil
}

// TokenGuard is the revocation check run on every authenticated request.
//
// It is a seam rather than a concrete check because what it consults — the
// permission registry, holding each role's current permission version and the
// set of suspended users — belongs to the RBAC module, which the shared kernel
// may not import.
//
// Returning an error rejects the request. Both gates it exists for are
// deliberately in-memory: an access token is only worth having if checking it
// costs no round trip, and a revocation check that hits the database on every
// request gives that back.
type TokenGuard interface {
	Check(claims *Claims) *errors.AppError
}

// AllowAllGuard accepts every otherwise-valid token.
//
// It is the default until the permission registry exists. **It is not a
// no-op you can leave in place**: with this guard a token stays valid for its
// full TTL after a role's permissions change or an account is suspended, which
// is precisely the weakness stateless tokens are known for. The RBAC phase
// replaces it, and the only reason it exists is that a graph must resolve.
type AllowAllGuard struct{}

// Check accepts everything.
func (AllowAllGuard) Check(*Claims) *errors.AppError { return nil }

// JWTMiddleware authenticates a request from its Authorization header.
//
// It is provided as a bare gin.HandlerFunc, and it is the **only** provider in
// the graph allowed to be one: fx keys providers by type, so a second bare
// gin.HandlerFunc collides with a duplicate-type error at startup. Every other
// middleware gets a named type.
func JWTMiddleware(codec *TokenCodec, guard TokenGuard) gin.HandlerFunc {
	return func(c *gin.Context) {
		var token string
		var err error
		token, err = bearerToken(c.GetHeader("Authorization"))
		if err != nil {
			abort(c, err)
			return
		}

		var claims *Claims
		claims, err = codec.Parse(token)
		if err != nil {
			abort(c, err)
			return
		}

		var revoked *errors.AppError = guard.Check(claims)
		if revoked != nil {
			abort(c, revoked)
			return
		}

		setClaims(c, claims)
		c.Next()
	}
}

// bearerToken extracts the credential from an Authorization header.
//
// The scheme is compared case-insensitively because RFC 7235 says it is
// case-insensitive, and clients do send "bearer".
func bearerToken(header string) (string, error) {
	if strings.TrimSpace(header) == "" {
		return "", errors.NewUnauthorizedError("authentication required", nil)
	}

	var scheme string
	var credential string
	var found bool
	scheme, credential, found = strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(credential) == "" {
		return "", errors.NewUnauthorizedError(
			"authorization header must be: Bearer <token>", nil)
	}

	return strings.TrimSpace(credential), nil
}

// abort renders a classified error and stops the chain.
//
// Like every handler, it asks the error for its status rather than choosing
// one — the middleware is not a special case.
func abort(c *gin.Context, err error) {
	var appErr *errors.AppError = errors.From(err)
	c.AbortWithStatusJSON(appErr.ToHTTPStatus(), appErr.Response())
}

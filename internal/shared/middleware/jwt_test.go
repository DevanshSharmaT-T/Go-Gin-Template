// File: internal/shared/middleware/jwt_test.go

package middleware

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

const testSecret = "a-test-jwt-signing-key-of-sufficient-length"

func testCodec() *TokenCodec {
	return NewTokenCodec(&config.Config{Auth: config.Auth{
		JWTSecret: testSecret,
		JWTIssuer: "test-issuer",
		JWTTTL:    time.Hour,
	}})
}

func testClaims() *Claims {
	return &Claims{
		UserID:         uuid.New(),
		RoleID:         4,
		HierarchyLevel: 100,
		Permissions:    map[string]bool{"users:read": true},
		PermVersion:    7,
	}
}

func init() { gin.SetMode(gin.TestMode) }

// request runs one request through JWTMiddleware and reports the status and the
// claims the middleware installed.
func request(t *testing.T, guard TokenGuard, header string) (int, *Claims) {
	t.Helper()

	var seen *Claims
	router := gin.New()
	router.GET("/", JWTMiddleware(testCodec(), guard), func(c *gin.Context) {
		claims, ok := ClaimsFrom(c)
		if ok {
			seen = claims
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec.Code, seen
}

func TestJWTMiddleware_AcceptsAValidToken(t *testing.T) {
	claims := testClaims()
	token, _, err := testCodec().Sign(claims)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	status, seen := request(t, AllowAllGuard{}, "Bearer "+token)
	if status != http.StatusOK {
		t.Fatalf("want 200, got %d", status)
	}
	if seen == nil {
		t.Fatal("the middleware did not install the claims")
	}
	if seen.UserID != claims.UserID || seen.PermVersion != 7 {
		t.Fatalf("claims did not survive the round trip: %+v", seen)
	}
	if !seen.HasPermission("users:read") {
		t.Fatal("the permission map did not survive the round trip")
	}
}

// RFC 7235 says the scheme is case-insensitive, and clients do send "bearer".
func TestJWTMiddleware_AcceptsAnyCaseOfTheBearerScheme(t *testing.T) {
	token, _, err := testCodec().Sign(testClaims())
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		t.Run(scheme, func(t *testing.T) {
			status, _ := request(t, AllowAllGuard{}, scheme+" "+token)
			if status != http.StatusOK {
				t.Fatalf("want 200 for scheme %q, got %d", scheme, status)
			}
		})
	}
}

func TestJWTMiddleware_RejectsMissingOrMalformedHeaders(t *testing.T) {
	cases := map[string]string{
		"absent":           "",
		"no scheme":        "sometoken",
		"wrong scheme":     "Basic sometoken",
		"scheme only":      "Bearer",
		"empty credential": "Bearer   ",
	}

	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			status, seen := request(t, AllowAllGuard{}, header)
			if status != http.StatusUnauthorized {
				t.Fatalf("want 401, got %d", status)
			}
			if seen != nil {
				t.Fatal("claims were installed for an unauthenticated request")
			}
		})
	}
}

// The classic attack: a token declaring `alg: none` and carrying no signature.
// A parser that trusts the token's own header to pick the algorithm accepts it.
func TestJWTMiddleware_RejectsTheNoneAlgorithm(t *testing.T) {
	claims := testClaims()
	claims.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    "test-issuer",
		Subject:   claims.UserID.String(),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}

	// jwt.UnsafeAllowNoneSignatureType is the library's explicit opt-in for
	// producing such a token; there is no way to make one by accident.
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("building the alg:none token: %v", err)
	}

	status, seen := request(t, AllowAllGuard{}, "Bearer "+token)
	if status != http.StatusUnauthorized {
		t.Fatalf("an alg:none token was accepted with status %d", status)
	}
	if seen != nil {
		t.Fatal("an alg:none token installed claims")
	}
}

// The other half of the same family: a token whose header names an algorithm
// the server does not use. Accepting it is what turns a public key into an
// HMAC secret in the RS256-to-HS256 confusion attack.
func TestJWTMiddleware_RejectsAnUnexpectedAlgorithm(t *testing.T) {
	claims := testClaims()
	claims.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    "test-issuer",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}

	// HS512 with the right key: correctly signed, wrong algorithm.
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	if status, _ := request(t, AllowAllGuard{}, "Bearer "+token); status != http.StatusUnauthorized {
		t.Fatalf("an HS512 token was accepted with status %d", status)
	}
}

func TestJWTMiddleware_RejectsAnotherKeysSignature(t *testing.T) {
	other := NewTokenCodec(&config.Config{Auth: config.Auth{
		JWTSecret: "a-completely-different-signing-key-entirely",
		JWTIssuer: "test-issuer",
		JWTTTL:    time.Hour,
	}})

	token, _, err := other.Sign(testClaims())
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	if status, _ := request(t, AllowAllGuard{}, "Bearer "+token); status != http.StatusUnauthorized {
		t.Fatalf("a foreign signature was accepted with status %d", status)
	}
}

// A token minted by another service that happens to share the secret must not
// be accepted here.
func TestJWTMiddleware_RejectsAForeignIssuer(t *testing.T) {
	foreign := NewTokenCodec(&config.Config{Auth: config.Auth{
		JWTSecret: testSecret,
		JWTIssuer: "some-other-service",
		JWTTTL:    time.Hour,
	}})

	token, _, err := foreign.Sign(testClaims())
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	if status, _ := request(t, AllowAllGuard{}, "Bearer "+token); status != http.StatusUnauthorized {
		t.Fatalf("a token from another issuer was accepted with status %d", status)
	}
}

func TestJWTMiddleware_RejectsAnExpiredToken(t *testing.T) {
	expired := NewTokenCodec(&config.Config{Auth: config.Auth{
		JWTSecret: testSecret,
		JWTIssuer: "test-issuer",
		JWTTTL:    -time.Minute,
	}})

	token, _, err := expired.Sign(testClaims())
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	if status, _ := request(t, AllowAllGuard{}, "Bearer "+token); status != http.StatusUnauthorized {
		t.Fatalf("an expired token was accepted with status %d", status)
	}

	// The classification tells a client to sign in again rather than to retry.
	_, err = expired.Parse(token)
	if !apperrors.IsType(err, apperrors.TypeTokenExpired) {
		t.Fatalf("want TOKEN_EXPIRED, got %v", err)
	}
}

// A token that simply omits `exp` must not be treated as one that never
// expires, which is what a parser without WithExpirationRequired does.
func TestJWTMiddleware_RejectsATokenWithNoExpiry(t *testing.T) {
	claims := testClaims()
	claims.RegisteredClaims = jwt.RegisteredClaims{Issuer: "test-issuer"}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	if status, _ := request(t, AllowAllGuard{}, "Bearer "+token); status != http.StatusUnauthorized {
		t.Fatalf("a token with no expiry was accepted with status %d", status)
	}
}

// The guard is the revocation seam. When it refuses, the request must stop even
// though the signature is perfectly good.
func TestJWTMiddleware_HonoursTheGuard(t *testing.T) {
	token, _, err := testCodec().Sign(testClaims())
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	status, seen := request(t, refusingGuard{}, "Bearer "+token)
	if status != http.StatusForbidden {
		t.Fatalf("want 403 from the guard, got %d", status)
	}
	if seen != nil {
		t.Fatal("a refused token installed claims")
	}
}

type refusingGuard struct{}

func (refusingGuard) Check(*Claims) *apperrors.AppError {
	return apperrors.NewUserSuspendedError("account is suspended", nil)
}

// A JWT is signed, not encrypted. This asserts the payload we choose to put in
// one carries nothing secret, since anyone holding it can read all of it.
func TestSign_PayloadCarriesNothingSecret(t *testing.T) {
	token, _, err := testCodec().Sign(testClaims())
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("want three JWT segments, got %d", len(parts))
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decoding the payload: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("the payload is not JSON: %v", err)
	}

	for _, forbidden := range []string{"password", "password_hash", "email", "secret", "token_secret"} {
		if _, present := payload[forbidden]; present {
			t.Fatalf("the token payload carries %q", forbidden)
		}
	}
}

// The four context keys are documented in ARCHITECTURE.md and asserted by
// downstream code, so their names and types are contract.
func TestJWTMiddleware_SetsTheDocumentedContextKeys(t *testing.T) {
	claims := testClaims()
	token, _, err := testCodec().Sign(claims)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	var got map[string]any
	router := gin.New()
	router.GET("/", JWTMiddleware(testCodec(), AllowAllGuard{}), func(c *gin.Context) {
		got = map[string]any{}
		for _, key := range []string{ContextUserID, ContextRoleID, ContextHierarchyLevel, ContextPermissions} {
			value, exists := c.Get(key)
			if !exists {
				t.Errorf("context key %q was not set", key)
				continue
			}
			got[key] = value
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(httptest.NewRecorder(), req)

	if _, ok := got[ContextUserID].(uuid.UUID); !ok {
		t.Fatalf("user_id must be a uuid.UUID, got %T", got[ContextUserID])
	}
	if _, ok := got[ContextRoleID].(int64); !ok {
		t.Fatalf("role_id must be an int64, got %T", got[ContextRoleID])
	}
	if _, ok := got[ContextHierarchyLevel].(int64); !ok {
		t.Fatalf("hierarchy_level must be an int64, got %T", got[ContextHierarchyLevel])
	}
	if _, ok := got[ContextPermissions].(map[string]bool); !ok {
		t.Fatalf("permissions must be a map[string]bool, got %T", got[ContextPermissions])
	}
}

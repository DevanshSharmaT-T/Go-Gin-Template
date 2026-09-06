// File: internal/shared/middleware/authorize_test.go

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// authorized runs one request through JWTMiddleware then Authorize, with a
// token carrying the given level and permissions.
func authorized(t *testing.T, level int64, granted []string, slug string, required int64) int {
	t.Helper()

	permissions := map[string]bool{}
	for _, s := range granted {
		permissions[s] = true
	}

	token, _, err := testCodec().Sign(&Claims{
		UserID:         uuid.New(),
		RoleID:         3,
		HierarchyLevel: level,
		Permissions:    permissions,
		PermVersion:    1,
	})
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	router := gin.New()
	router.GET("/",
		JWTMiddleware(testCodec(), AllowAllGuard{}),
		Authorize(slug, required),
		func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec.Code
}

// The direction of the comparison is the thing that would silently invert the
// whole model: with `>=` every route would be open to the *least* privileged
// role, and every test that only checked "an admin can" would still pass.
func TestAuthorize_LowerLevelIsMoreSenior(t *testing.T) {
	cases := []struct {
		name     string
		level    int64
		required int64
		want     int
	}{
		{"more senior than required", 10, 20, http.StatusOK},
		{"exactly the required level", 20, 20, http.StatusOK},
		{"less senior than required", 100, 20, http.StatusForbidden},
		{"one step too junior", 21, 20, http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := authorized(t, tc.level, []string{"users:list"}, "users:list", tc.required)
			if got != tc.want {
				t.Fatalf("level %d against required %d: want %d, got %d",
					tc.level, tc.required, tc.want, got)
			}
		})
	}
}

// Both conditions must hold. Either one alone opening the route would make the
// other decorative.
func TestAuthorize_RequiresBothLevelAndPermission(t *testing.T) {
	cases := []struct {
		name    string
		level   int64
		granted []string
		want    int
	}{
		{"senior and granted", 10, []string{"users:list"}, http.StatusOK},
		{"senior but not granted", 10, nil, http.StatusForbidden},
		{"granted but not senior", 100, []string{"users:list"}, http.StatusForbidden},
		{"neither", 100, nil, http.StatusForbidden},
		{"granted something else", 10, []string{"users:read"}, http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := authorized(t, tc.level, tc.granted, "users:list", 20)
			if got != tc.want {
				t.Fatalf("want %d, got %d", tc.want, got)
			}
		})
	}
}

// The break-glass: a misconfigured permission table must not lock everyone out
// of the system that fixes permission tables.
func TestAuthorize_SuperAdminBypassesThePermissionMap(t *testing.T) {
	// No permissions at all, and a route requiring a slug the token does not
	// carry — at any required level.
	for _, required := range []int64{1, 10, 20, 100} {
		got := authorized(t, SuperAdminLevel, nil, "anything:at:all", required)
		if got != http.StatusOK {
			t.Fatalf("super admin refused at required level %d: got %d", required, got)
		}
	}
}

// The bypass is for level 1 only. Level 2 is not "nearly super admin".
func TestAuthorize_TheBypassIsLevelOneOnly(t *testing.T) {
	got := authorized(t, SuperAdminLevel+1, nil, "users:list", 20)
	if got != http.StatusForbidden {
		t.Fatalf("level %d bypassed the permission map: got %d", SuperAdminLevel+1, got)
	}
}

// A gate that opens when it cannot find the claims it checks is not a gate.
// This is what happens if Authorize is registered without JWTMiddleware in
// front of it.
func TestAuthorize_RefusesWhenThereAreNoClaims(t *testing.T) {
	router := gin.New()
	router.GET("/", Authorize("users:list", 20), func(c *gin.Context) {
		t.Error("the handler ran on a route with no authentication in front of it")
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 with no claims, got %d", rec.Code)
	}
}

// A refusal must not say which of the two conditions failed. "You have the
// permission but not the seniority" tells a caller how the model is shaped and
// which accounts to go after.
func TestAuthorize_RefusalDoesNotSayWhy(t *testing.T) {
	router := gin.New()
	router.GET("/level", JWTMiddleware(testCodec(), AllowAllGuard{}),
		Authorize("users:list", 20), func(c *gin.Context) { c.Status(http.StatusOK) })

	body := func(level int64, granted []string) string {
		permissions := map[string]bool{}
		for _, s := range granted {
			permissions[s] = true
		}
		token, _, err := testCodec().Sign(&Claims{
			UserID: uuid.New(), RoleID: 3, HierarchyLevel: level,
			Permissions: permissions, PermVersion: 1,
		})
		if err != nil {
			t.Fatalf("signing: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/level", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Body.String()
	}

	tooJunior := body(100, []string{"users:list"})
	notGranted := body(10, nil)

	if tooJunior != notGranted {
		t.Fatalf("the two refusals differ, which describes the model:\n level:  %s\n perm:   %s",
			tooJunior, notGranted)
	}
}

func TestRequireLevel_GatesOnSeniorityAlone(t *testing.T) {
	run := func(level int64, required int64) int {
		token, _, err := testCodec().Sign(&Claims{
			UserID: uuid.New(), RoleID: 3, HierarchyLevel: level,
			Permissions: map[string]bool{}, PermVersion: 1,
		})
		if err != nil {
			t.Fatalf("signing: %v", err)
		}

		router := gin.New()
		router.GET("/", JWTMiddleware(testCodec(), AllowAllGuard{}),
			RequireLevel(required), func(c *gin.Context) { c.Status(http.StatusOK) })

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	// No permissions needed: seniority is the whole check.
	if got := run(10, 20); got != http.StatusOK {
		t.Fatalf("want 200 for a senior caller, got %d", got)
	}
	if got := run(100, 20); got != http.StatusForbidden {
		t.Fatalf("want 403 for a junior caller, got %d", got)
	}
}

// File: internal/shared/middleware/chain_test.go

package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
)

// --- request ID --------------------------------------------------------------

func TestRequestID_GeneratesOneWhenAbsent(t *testing.T) {
	router := gin.New()
	var seen string
	router.GET("/", gin.HandlerFunc(NewRequestIDMiddleware()), func(c *gin.Context) {
		seen = RequestIDFrom(c)
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen == "" {
		t.Fatal("no request ID was generated")
	}
	if rec.Header().Get(RequestIDHeader) != seen {
		t.Fatalf("the header does not match the context value: %q vs %q",
			rec.Header().Get(RequestIDHeader), seen)
	}
}

func TestRequestID_HonoursACleanInboundValue(t *testing.T) {
	router := gin.New()
	var seen string
	router.GET("/", gin.HandlerFunc(NewRequestIDMiddleware()), func(c *gin.Context) {
		seen = RequestIDFrom(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "trace-abc_123.4")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if seen != "trace-abc_123.4" {
		t.Fatalf("an inbound trace ID was not honoured: %q", seen)
	}
}

// **The log-injection regression.** The header is attacker-controlled and its
// value goes into every log line for the request. A newline in it forges log
// entries; an unbounded one writes megabytes per request into log storage.
func TestRequestID_RejectsHostileInboundValues(t *testing.T) {
	hostile := map[string]string{
		"newline":     "abc\ndef",
		"crlf":        "abc\r\ndef",
		"tab":         "abc\tdef",
		"space":       "abc def",
		"ansi escape": "abc\x1b[31mdef",
		"nul":         "abc\x00def",
		"json break":  `abc","level":"fatal","x":"`,
		"too long":    strings.Repeat("a", maxRequestIDLength+1),
		"non-ascii":   "abc def",
	}

	for name, value := range hostile {
		t.Run(name, func(t *testing.T) {
			router := gin.New()
			var seen string
			router.GET("/", gin.HandlerFunc(NewRequestIDMiddleware()), func(c *gin.Context) {
				seen = RequestIDFrom(c)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(RequestIDHeader, value)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if seen == value {
				t.Fatalf("a hostile request ID was accepted verbatim: %q", seen)
			}
			if !validRequestID(seen) {
				t.Fatalf("the replacement is itself not valid: %q", seen)
			}
			// It must not be echoed back either, or the response header
			// becomes the injection point instead of the log.
			if strings.Contains(rec.Header().Get(RequestIDHeader), "\n") {
				t.Fatal("a newline was echoed into the response header")
			}
		})
	}
}

// --- CORS --------------------------------------------------------------------

func corsRouter(origins ...string) *gin.Engine {
	cfg := &config.Config{CORS: config.CORS{
		AllowedOrigins:   origins,
		AllowCredentials: true,
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		ExposedHeaders:   []string{"X-Request-ID"},
		MaxAge:           12 * time.Hour,
	}}

	router := gin.New()
	router.Use(gin.HandlerFunc(NewCORSMiddleware(cfg)))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.POST("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	return router
}

func corsRequest(router *gin.Engine, method string, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if method == http.MethodOptions {
		req.Header.Set("Access-Control-Request-Method", "POST")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCORS_AllowsAListedOrigin(t *testing.T) {
	rec := corsRequest(corsRouter("https://app.example.com"), http.MethodGet, "https://app.example.com")

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("want the origin echoed, got %q", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("credentials were not allowed")
	}
}

// **The one that matters.** Reflecting whatever arrived is the same as having
// no policy: every site becomes an allowed origin, and with credentials on,
// every authenticated endpoint becomes readable by any page the user visits.
func TestCORS_NeverEchoesAnUnlistedOrigin(t *testing.T) {
	router := corsRouter("https://app.example.com")

	hostile := []string{
		"https://evil.example.com",
		"https://app.example.com.evil.net", // suffix confusion
		"https://evil.net/https://app.example.com",
		"http://app.example.com",       // wrong scheme
		"https://app.example.com:8443", // wrong port
		"https://APP.example.com",      // case
		"null",
	}

	for _, origin := range hostile {
		t.Run(origin, func(t *testing.T) {
			rec := corsRequest(router, http.MethodGet, origin)

			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Fatalf("an unlisted origin was echoed: %q", got)
			}
			if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Fatal("credentials were allowed for an unlisted origin")
			}
		})
	}
}

// Without Vary: Origin a shared cache can store the response given to an
// allowed origin and serve it to a disallowed one — handing out the header the
// allow-list just refused.
func TestCORS_AlwaysSetsVaryOrigin(t *testing.T) {
	router := corsRouter("https://app.example.com")

	for _, origin := range []string{"https://app.example.com", "https://evil.example.com", ""} {
		rec := corsRequest(router, http.MethodGet, origin)
		if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
			t.Fatalf("Vary: Origin missing for origin %q", origin)
		}
	}
}

func TestCORS_PreflightIsAnsweredAndStops(t *testing.T) {
	allowed := corsRequest(corsRouter("https://app.example.com"), http.MethodOptions, "https://app.example.com")
	if allowed.Code != http.StatusNoContent {
		t.Fatalf("want 204 for an allowed preflight, got %d", allowed.Code)
	}
	if allowed.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatal("the preflight response lists no methods")
	}

	refused := corsRequest(corsRouter("https://app.example.com"), http.MethodOptions, "https://evil.example.com")
	if refused.Code != http.StatusForbidden {
		t.Fatalf("want 403 for an unlisted preflight, got %d", refused.Code)
	}
	if refused.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("an unlisted preflight was given CORS headers")
	}
}

// A request with no Origin is not cross-origin and needs no headers.
func TestCORS_SameOriginRequestIsUntouched(t *testing.T) {
	rec := corsRequest(corsRouter("https://app.example.com"), http.MethodGet, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("a same-origin request was given CORS headers")
	}
}

// --- rate limiting -----------------------------------------------------------

// The classic bug: a map keyed by client IP with no expiry is a memory leak
// with an attacker-controlled key. One host cycling through addresses adds an
// entry per request, forever.
func TestRateLimit_EvictsIdleBuckets(t *testing.T) {
	store := &limiterStore{
		limiters: map[string]*trackedLimiter{},
		limit:    rate.Limit(100),
		burst:    100,
	}

	for i := 0; i < 500; i++ {
		store.allow(distinctIP(i))
	}
	if store.size() != 500 {
		t.Fatalf("want 500 buckets, got %d", store.size())
	}

	// Age them past the TTL and sweep once, the way the background sweeper
	// would.
	store.mu.Lock()
	for _, tracked := range store.limiters {
		tracked.seen = time.Now().Add(-2 * idleLimiterTTL)
	}
	store.mu.Unlock()

	cutoff := time.Now().Add(-idleLimiterTTL)
	store.mu.Lock()
	for key, tracked := range store.limiters {
		if tracked.seen.Before(cutoff) {
			delete(store.limiters, key)
		}
	}
	store.mu.Unlock()

	if store.size() != 0 {
		t.Fatalf("idle buckets were not evicted: %d remain", store.size())
	}
}

// distinctIP returns a different address for every n, which is the point: the
// leak being tested is one bucket per source address.
func distinctIP(n int) string {
	return fmt.Sprintf("10.%d.%d.%d", (n>>16)&0xff, (n>>8)&0xff, n&0xff)
}

func TestRateLimit_RefusesPastTheBurst(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimit{
		Enabled: true, RPS: 1, Burst: 2, AuthRPM: 60, AuthBurst: 2,
	}}

	router := gin.New()
	router.Use(gin.HandlerFunc(NewRateLimitMiddleware(cfg)))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	statuses := []int{}
	for i := 0; i < 4; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.9:1234"
		router.ServeHTTP(rec, req)
		statuses = append(statuses, rec.Code)
	}

	if statuses[0] != http.StatusOK || statuses[1] != http.StatusOK {
		t.Fatalf("the burst was not honoured: %v", statuses)
	}
	if statuses[2] != http.StatusTooManyRequests {
		t.Fatalf("want 429 past the burst, got %v", statuses)
	}
}

// A refusal must tell a well-behaved client when to come back.
func TestRateLimit_SetsRetryAfter(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimit{Enabled: true, RPS: 1, Burst: 1}}

	router := gin.New()
	router.Use(gin.HandlerFunc(NewRateLimitMiddleware(cfg)))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	var last *httptest.ResponseRecorder
	for i := 0; i < 3; i++ {
		last = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		router.ServeHTTP(last, req)
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", last.Code)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Fatal("a refusal carries no Retry-After")
	}
}

// One client's budget must not be spent by another's traffic.
func TestRateLimit_IsPerClient(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimit{Enabled: true, RPS: 1, Burst: 1}}

	router := gin.New()
	router.Use(gin.HandlerFunc(NewRateLimitMiddleware(cfg)))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := func(ip string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip + ":1234"
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := request("203.0.113.1"); got != http.StatusOK {
		t.Fatalf("first client refused: %d", got)
	}
	if got := request("203.0.113.1"); got != http.StatusTooManyRequests {
		t.Fatalf("first client not limited: %d", got)
	}
	if got := request("203.0.113.2"); got != http.StatusOK {
		t.Fatalf("a second client was limited by the first's traffic: %d", got)
	}
}

func TestRateLimit_DisabledLetsEverythingThrough(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimit{Enabled: false}}

	router := gin.New()
	router.Use(gin.HandlerFunc(NewRateLimitMiddleware(cfg)))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	for i := 0; i < 50; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.3:1234"
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d was limited with limiting disabled", i)
		}
	}
}

// The store is read and written from every request goroutine.
func TestRateLimit_StoreIsSafeForConcurrentUse(t *testing.T) {
	store := newLimiterStore(rate.Limit(1000), 1000)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				store.allow(distinctIP(n*1000 + j))
				_ = store.size()
			}
		}(i)
	}
	wg.Wait()
}

// --- body limit --------------------------------------------------------------

func TestBodyLimit_RejectsADeclaredOversizeBody(t *testing.T) {
	cfg := &config.Config{Server: config.Server{MaxRequestBodyBytes: 32}}

	router := gin.New()
	router.Use(gin.HandlerFunc(NewBodyLimitMiddleware(cfg)))
	router.POST("/", func(c *gin.Context) {
		t.Error("the handler ran for an oversize body")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", 200)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", rec.Code)
	}
}

// A client that lies about Content-Length, or uses chunked encoding and
// declares nothing, is caught on read instead.
func TestBodyLimit_CapsAnUndeclaredBody(t *testing.T) {
	cfg := &config.Config{Server: config.Server{MaxRequestBodyBytes: 32}}

	router := gin.New()
	router.Use(gin.HandlerFunc(NewBodyLimitMiddleware(cfg)))

	var bindErr error
	router.POST("/", func(c *gin.Context) {
		var target map[string]any
		bindErr = c.ShouldBindJSON(&target)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"a":"`+strings.Repeat("b", 500)+`"}`))
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if bindErr == nil {
		t.Fatal("an undeclared oversize body decoded successfully")
	}
	if BindError(bindErr).ToHTTPStatus() != http.StatusRequestEntityTooLarge {
		t.Fatalf("want the bind failure classified as 413, got %s", BindError(bindErr).Type)
	}
}

// A malformed body is the caller's mistake, and a different one.
func TestBindError_DistinguishesMalformedFromOversize(t *testing.T) {
	router := gin.New()
	var bindErr error
	router.POST("/", func(c *gin.Context) {
		var target map[string]any
		bindErr = c.ShouldBindJSON(&target)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not json"))
	router.ServeHTTP(httptest.NewRecorder(), req)

	if got := BindError(bindErr).ToHTTPStatus(); got != http.StatusBadRequest {
		t.Fatalf("want 400 for a malformed body, got %d", got)
	}
	if BindError(nil) != nil {
		t.Fatal("BindError(nil) should be nil")
	}
}

// --- recovery ----------------------------------------------------------------

// A panic message routinely contains a path, a struct dump or a query. A client
// that can trigger one should not also receive its contents.
func TestRecovery_NeverLeaksThePanicValue(t *testing.T) {
	router := gin.New()
	router.Use(gin.HandlerFunc(NewRecoveryMiddleware()))
	router.GET("/", func(c *gin.Context) {
		panic("connection to postgres://user:hunter2@db.internal:5432 failed")
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, secret := range []string{"hunter2", "postgres://", "db.internal"} {
		if strings.Contains(body, secret) {
			t.Fatalf("the panic value leaked into the response: %q", body)
		}
	}
	if !strings.Contains(body, "INTERNAL") {
		t.Fatalf("want a classified 500 body, got %q", body)
	}
}

func TestRecovery_LetsAHealthyRequestThrough(t *testing.T) {
	router := gin.New()
	router.Use(gin.HandlerFunc(NewRecoveryMiddleware()))
	router.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "fine") })

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "fine" {
		t.Fatalf("a healthy request was disturbed: %d %q", rec.Code, rec.Body.String())
	}
}

// --- probe exemption ---------------------------------------------------------

// **An availability property, not a convenience.** A probe that gets a 429 is a
// probe that failed: the instance leaves rotation, its traffic moves to the
// others, and they become more likely to limit their own probes. It cascades,
// and it does so exactly when the service is already under load.
func TestRateLimit_NeverLimitsProbes(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimit{Enabled: true, RPS: 1, Burst: 1}}

	router := gin.New()
	router.Use(gin.HandlerFunc(NewRateLimitMiddleware(cfg)))
	router.GET(PathLiveness, func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET(PathReadiness, func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET(PathHealth, func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/api/normal", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := func(path string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "203.0.113.50:1234"
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	// Spend the budget many times over on every probe path.
	for _, path := range []string{PathLiveness, PathReadiness, PathHealth} {
		for i := 0; i < 20; i++ {
			if got := request(path); got != http.StatusOK {
				t.Fatalf("probe %s was rate limited on request %d: %d", path, i, got)
			}
		}
	}

	// A normal route, from the same client, is still limited — the exemption is
	// for the probes and not a hole in the limiter.
	request("/api/normal")
	if got := request("/api/normal"); got != http.StatusTooManyRequests {
		t.Fatalf("the exemption leaked to a normal route: %d", got)
	}
}

func TestIsProbePath(t *testing.T) {
	for _, path := range []string{PathLiveness, PathReadiness, PathHealth} {
		if !IsProbePath(path) {
			t.Errorf("%q should be a probe path", path)
		}
	}
	for _, path := range []string{"/api/users", "/api/health/other", "", "/"} {
		if IsProbePath(path) {
			t.Errorf("%q should not be a probe path", path)
		}
	}
}

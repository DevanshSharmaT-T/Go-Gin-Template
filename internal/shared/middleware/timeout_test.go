// File: internal/shared/middleware/timeout_test.go

package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
)

func timeoutConfig(d time.Duration) *config.Config {
	return &config.Config{Server: config.Server{RequestTimeout: d}}
}

// run drives one request through the timeout middleware.
func run(t *testing.T, timeout time.Duration, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()

	router := gin.New()
	router.GET("/", gin.HandlerFunc(NewTimeoutMiddleware(timeoutConfig(timeout))), handler)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec
}

func TestTimeoutMiddleware_PassesAFastHandlerThrough(t *testing.T) {
	rec := run(t, time.Second, func(c *gin.Context) {
		c.Header("X-Custom", "kept")
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d", rec.Code)
	}
	if rec.Header().Get("X-Custom") != "kept" {
		t.Fatal("a header set by the handler was dropped")
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the buffered body did not survive: %v", err)
	}
	if body["ok"] != true {
		t.Fatalf("body: got %v", body)
	}
}

func TestTimeoutMiddleware_SlowHandlerGetsA504(t *testing.T) {
	rec := run(t, 30*time.Millisecond, func(c *gin.Context) {
		time.Sleep(300 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"too": "late"})
	})

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("want 504, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the timeout response is not JSON: %q", rec.Body.String())
	}
	if body["type"] != "TIMEOUT" {
		t.Fatalf("want a TIMEOUT classification, got %v", body["type"])
	}
	// The handler's response must not be there as well.
	if _, present := body["too"]; present {
		t.Fatal("the abandoned handler's body was written alongside the timeout")
	}
}

// The deadline has to reach the handler, or a slow query keeps running for a
// client who is already gone.
func TestTimeoutMiddleware_CancelsTheRequestContext(t *testing.T) {
	var seen error
	var mu sync.Mutex

	rec := run(t, 30*time.Millisecond, func(c *gin.Context) {
		<-c.Request.Context().Done()
		mu.Lock()
		seen = c.Request.Context().Err()
		mu.Unlock()
	})

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("want 504, got %d", rec.Code)
	}

	mu.Lock()
	defer mu.Unlock()
	if seen != context.DeadlineExceeded {
		t.Fatalf("the handler's context was not cancelled with a deadline: %v", seen)
	}
}

// **The race this middleware exists to close.** The abandoned handler is still
// running and about to write. Under -race, a second writer on the real
// ResponseWriter shows up here.
func TestTimeoutMiddleware_AbandonedHandlerCannotWrite(t *testing.T) {
	wrote := make(chan struct{})

	router := gin.New()
	router.GET("/", gin.HandlerFunc(NewTimeoutMiddleware(timeoutConfig(20*time.Millisecond))),
		func(c *gin.Context) {
			// Keep writing after the timeout has certainly fired.
			time.Sleep(80 * time.Millisecond)
			for i := 0; i < 50; i++ {
				c.Header("X-Late", "yes")
				c.String(http.StatusOK, "late output that must be discarded")
			}
			close(wrote)
		})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("want 504, got %d", rec.Code)
	}

	// Wait for the abandoned handler to finish writing into the void, so the
	// race detector has seen every write it makes.
	select {
	case <-wrote:
	case <-time.After(2 * time.Second):
		t.Fatal("the abandoned handler never finished")
	}

	if rec.Header().Get("X-Late") != "" {
		t.Fatal("a header set after the timeout reached the response")
	}
	if body := rec.Body.String(); len(body) == 0 || body[0] != '{' {
		t.Fatalf("the response body is not the timeout payload: %q", body)
	}
}

// A panic on the handler's goroutine would take the process down. It has to be
// re-raised on the middleware's goroutine, where recovery can see it.
func TestTimeoutMiddleware_RepanicsOnTheOuterGoroutine(t *testing.T) {
	router := gin.New()
	router.Use(gin.HandlerFunc(NewRecoveryMiddleware()))
	router.GET("/", gin.HandlerFunc(NewTimeoutMiddleware(timeoutConfig(time.Second))),
		func(c *gin.Context) {
			panic("handler exploded")
		})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 from recovery, got %d", rec.Code)
	}
	if body := rec.Body.String(); body == "" {
		t.Fatal("no body was written for the recovered panic")
	}
}

// Zero disables it rather than timing out every request instantly.
func TestTimeoutMiddleware_ZeroDisablesIt(t *testing.T) {
	rec := run(t, 0, func(c *gin.Context) {
		time.Sleep(20 * time.Millisecond)
		c.String(http.StatusOK, "fine")
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 with the timeout disabled, got %d", rec.Code)
	}
}

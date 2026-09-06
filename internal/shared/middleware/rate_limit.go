// File: internal/shared/middleware/rate_limit.go

package middleware

import (
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// RateLimitMiddleware is the global per-IP bucket.
type RateLimitMiddleware gin.HandlerFunc

// AuthRateLimitMiddleware is the tighter bucket for the unauthenticated auth
// routes, where credential stuffing is the thing being slowed down.
type AuthRateLimitMiddleware gin.HandlerFunc

// Eviction settings for the limiter store.
const (
	// idleLimiterTTL is how long a client's bucket is kept after its last
	// request. Long enough that a normal browsing session keeps one bucket,
	// short enough that a scan across many addresses does not accumulate.
	idleLimiterTTL = 10 * time.Minute

	// sweepInterval is how often expired buckets are collected.
	sweepInterval = time.Minute
)

// limiterStore holds one token bucket per client.
//
// **The eviction is the part that matters.** A map keyed by client IP, with no
// expiry, is a memory leak with an attacker-controlled key: every new source
// address adds an entry that is never removed, and a single host cycling
// through a /64 of IPv6 addresses can add millions. Buckets idle for
// idleLimiterTTL are dropped by a sweeper, which is also correct behaviour —
// a bucket that has been idle that long has refilled anyway, so nothing is lost
// by forgetting it.
type limiterStore struct {
	mu       sync.Mutex
	limiters map[string]*trackedLimiter

	limit rate.Limit
	burst int
}

// trackedLimiter is a bucket plus when it was last used.
type trackedLimiter struct {
	limiter *rate.Limiter
	seen    time.Time
}

// newLimiterStore builds a store and starts its sweeper.
//
// The sweeper goroutine runs for the life of the process, which is the right
// lifetime for it — there is one store per middleware and they are built once
// at startup.
func newLimiterStore(limit rate.Limit, burst int) *limiterStore {
	var store *limiterStore = &limiterStore{
		limiters: map[string]*trackedLimiter{},
		limit:    limit,
		burst:    burst,
	}

	go store.sweep()
	return store
}

// allow reports whether a request from key may proceed, and how long to wait if
// not.
func (s *limiterStore) allow(key string) (bool, time.Duration) {
	s.mu.Lock()
	var tracked *trackedLimiter
	var found bool
	tracked, found = s.limiters[key]
	if !found {
		tracked = &trackedLimiter{limiter: rate.NewLimiter(s.limit, s.burst)}
		s.limiters[key] = tracked
	}
	tracked.seen = time.Now()
	var limiter *rate.Limiter = tracked.limiter
	s.mu.Unlock()

	// Reserve rather than Allow, so a refusal can say when to come back. The
	// reservation is cancelled immediately when it cannot be honoured now,
	// which puts the token back for whoever asks next.
	var reservation *rate.Reservation = limiter.Reserve()
	if !reservation.OK() {
		return false, 0
	}

	var wait time.Duration = reservation.Delay()
	if wait > 0 {
		reservation.Cancel()
		return false, wait
	}
	return true, 0
}

// sweep drops buckets nobody has used recently.
func (s *limiterStore) sweep() {
	var ticker *time.Ticker = time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for range ticker.C {
		var cutoff time.Time = time.Now().Add(-idleLimiterTTL)

		s.mu.Lock()
		var key string
		var tracked *trackedLimiter
		for key, tracked = range s.limiters {
			if tracked.seen.Before(cutoff) {
				delete(s.limiters, key)
			}
		}
		s.mu.Unlock()
	}
}

// size reports how many buckets are held. It exists for the test that checks
// eviction actually happens.
func (s *limiterStore) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.limiters)
}

// NewRateLimitMiddleware builds the global limiter.
//
// The key is c.ClientIP(), which honours TRUSTED_PROXIES. That default — trust
// nothing — is what stops a client from choosing its own key: with a blanket
// trust of X-Forwarded-For, an attacker sends a different value on every
// request and the limiter counts each as a new client, which is the same as not
// having one.
func NewRateLimitMiddleware(cfg *config.Config) RateLimitMiddleware {
	if !cfg.RateLimit.Enabled {
		return func(c *gin.Context) { c.Next() }
	}

	var store *limiterStore = newLimiterStore(rate.Limit(cfg.RateLimit.RPS), cfg.RateLimit.Burst)
	return func(c *gin.Context) {
		enforce(c, store)
	}
}

// NewAuthRateLimitMiddleware builds the tighter limiter for the auth routes.
//
// It is configured per *minute* rather than per second, because the useful
// setting here is "ten login attempts an hour" rather than a rate anyone would
// express in requests per second.
func NewAuthRateLimitMiddleware(cfg *config.Config) AuthRateLimitMiddleware {
	if !cfg.RateLimit.Enabled {
		return func(c *gin.Context) { c.Next() }
	}

	var perSecond rate.Limit = rate.Limit(cfg.RateLimit.AuthRPM / 60.0)
	var store *limiterStore = newLimiterStore(perSecond, cfg.RateLimit.AuthBurst)
	return func(c *gin.Context) {
		enforce(c, store)
	}
}

// enforce applies a store to a request.
func enforce(c *gin.Context, store *limiterStore) {
	// Health probes are never limited. See IsProbePath for why that is an
	// availability requirement and not a convenience.
	if IsProbePath(c.FullPath()) {
		c.Next()
		return
	}

	var allowed bool
	var wait time.Duration
	allowed, wait = store.allow(c.ClientIP())

	if allowed {
		c.Next()
		return
	}

	// Retry-After tells a well-behaved client when to come back, which is the
	// difference between a client that backs off and one that hammers.
	var seconds int = int(wait.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	c.Header("Retry-After", strconv.Itoa(seconds))

	abort(c, apperrors.NewRateLimitedError("too many requests; slow down", nil).
		WithRequestID(RequestIDFrom(c)))
}

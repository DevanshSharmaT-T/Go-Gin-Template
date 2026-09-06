// File: internal/shared/middleware/probes.go

package middleware

// Probe paths. They are declared here, in the shared kernel, because two
// unrelated places need the same list: the health module registers routes at
// them, and the rate limiter exempts them. One list, both consumers — the same
// reason permission slugs are constants rather than literals.
const (
	PathLiveness  = "/healthz"
	PathReadiness = "/readyz"
	PathHealth    = "/api/health"
)

// probePaths is the set the rate limiter skips.
var probePaths map[string]bool = map[string]bool{
	PathLiveness:  true,
	PathReadiness: true,
	PathHealth:    true,
}

// IsProbePath reports whether a route is an orchestrator's health probe.
//
// **Probes are exempt from rate limiting, and that is an availability decision
// rather than a convenience.** A probe that gets a 429 is a probe that failed:
// the orchestrator marks the instance unhealthy and takes it out of rotation,
// which moves its traffic onto the remaining instances and makes them more
// likely to rate-limit their own probes. The failure cascades, and it does so
// precisely when the service is already under load.
//
// It is not hypothetical. With TRUSTED_PROXIES unset — the default, and the
// right one — every request behind a load balancer appears to come from the
// balancer's address, so the whole deployment shares one bucket and the probe
// competes with all of production for it.
//
// The paths are exempt from *rate limiting* only. They still pass through
// recovery, the request ID, logging and the timeout.
func IsProbePath(path string) bool {
	return probePaths[path]
}

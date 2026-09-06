// File: internal/modules/health/service/health_service.go

package service

import (
	"context"
	"time"

	"gorm.io/gorm"

	rolesdomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// The health module has no domain/ and no infra/, and that is not an omission.
// It owns no entity and stores nothing; it reports on dependencies other
// modules own. Giving it a repository would mean inventing persistence for a
// module whose whole job is to ask other things whether they are working.

// Status is a check's outcome.
type Status string

const (
	StatusOK   Status = "ok"
	StatusFail Status = "fail"
)

// String makes Status printable.
func (s Status) String() string { return string(s) }

// Check is one dependency's result.
//
// **There is no error field.** A failing check reports that it failed and how
// long it took, and the reason goes to the log. These endpoints are
// unauthenticated by necessity — an orchestrator cannot hold a credential — so
// anything they return is public, and a driver error names the host, the
// database and often the schema.
type Check struct {
	Name     string
	Status   Status
	Duration time.Duration
}

// Report is the outcome of every check.
type Report struct {
	Status Status
	Checks []Check
	Uptime time.Duration
}

// Healthy reports whether every check passed.
func (r *Report) Healthy() bool { return r.Status == StatusOK }

// checkTimeout bounds each dependency check.
//
// It is short on purpose. A readiness probe that hangs is worse than one that
// fails: the orchestrator waits for its own timeout, during which the instance
// is neither in rotation nor reported as broken.
const checkTimeout = 2 * time.Second

// HealthService answers the probes.
type HealthService struct {
	db       *gorm.DB
	registry *rolesdomain.PermissionRegistry
	started  time.Time
}

// NewHealthService builds the service.
func NewHealthService(db *gorm.DB) *HealthService {
	return &HealthService{
		db:       db,
		registry: rolesdomain.Registry,
		started:  time.Now(),
	}
}

// Live reports whether the process is running.
//
// It checks nothing, and that is the entire point of a liveness probe: it
// answers "is this process wedged, should it be restarted?". Checking the
// database here is the classic mistake — a database outage would fail every
// instance's liveness probe, the orchestrator would restart them all, and
// restarting an application server does not fix a database.
func (s *HealthService) Live() *Report {
	return &Report{
		Status: StatusOK,
		Checks: []Check{},
		Uptime: time.Since(s.started),
	}
}

// Ready reports whether this instance can serve a request.
//
// This is where dependencies belong. A failing readiness probe takes the
// instance out of rotation without restarting it, which is the correct response
// to "the database is unreachable from here" — the process is fine and will
// serve again when the dependency returns.
func (s *HealthService) Ready(ctx context.Context) *Report {
	var checkCtx context.Context
	var cancel context.CancelFunc
	checkCtx, cancel = context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	var report *Report = &Report{
		Status: StatusOK,
		Checks: []Check{
			s.checkDatabase(checkCtx),
			s.checkRegistry(),
		},
		Uptime: time.Since(s.started),
	}

	var check Check
	for _, check = range report.Checks {
		if check.Status != StatusOK {
			report.Status = StatusFail
		}
	}
	return report
}

// checkDatabase pings the pool.
func (s *HealthService) checkDatabase(ctx context.Context) Check {
	var started time.Time = time.Now()

	var err error = database.Ping(ctx, s.db)
	if err != nil {
		// The reason is logged and not returned; see Check.
		logger.FromContext(ctx).Error().Err(err).Msg("readiness check failed: database")
		return Check{Name: "database", Status: StatusFail, Duration: time.Since(started)}
	}

	return Check{Name: "database", Status: StatusOK, Duration: time.Since(started)}
}

// checkRegistry reports whether the permission registry has been warmed.
//
// An empty registry means authorization would refuse every gated route, so the
// instance is running but cannot usefully serve. That is exactly what readiness
// is for, and without this check the failure would look like a permissions bug
// to whoever hit it.
func (s *HealthService) checkRegistry() Check {
	var started time.Time = time.Now()

	if s.registry.Roles() == 0 {
		return Check{Name: "permissions", Status: StatusFail, Duration: time.Since(started)}
	}
	return Check{Name: "permissions", Status: StatusOK, Duration: time.Since(started)}
}

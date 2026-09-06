// File: test/integration/health_test.go

//go:build integration

package integration

import (
	"context"
	"testing"

	healthservice "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/health/service"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/test/harness"
)

// Against the real graph and a real database, with the registry warmed by the
// boot sequence.
func TestHealth_ReadyAgainstABootedApplication(t *testing.T) {
	var health *healthservice.HealthService
	harness.App(t, []any{&health})

	report := health.Ready(context.Background())

	if !report.Healthy() {
		t.Fatalf("a fully booted application is not ready: %+v", report.Checks)
	}

	names := map[string]healthservice.Status{}
	for _, check := range report.Checks {
		names[check.Name] = check.Status
	}

	for _, name := range []string{"database", "permissions"} {
		status, ran := names[name]
		if !ran {
			t.Errorf("readiness did not check %q", name)
			continue
		}
		if status != healthservice.StatusOK {
			t.Errorf("check %q reported %s against a healthy application", name, status)
		}
	}
}

// Liveness answers the same whatever the dependencies are doing.
func TestHealth_LiveIsIndependentOfDependencies(t *testing.T) {
	var health *healthservice.HealthService
	harness.App(t, []any{&health})

	report := health.Live()

	if !report.Healthy() {
		t.Fatal("liveness failed on a healthy application")
	}
	if len(report.Checks) != 0 {
		t.Fatalf("liveness ran %d dependency checks; it should run none", len(report.Checks))
	}
	if report.Uptime <= 0 {
		t.Fatal("liveness reports no uptime")
	}
}

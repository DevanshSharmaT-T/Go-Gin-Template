// File: internal/modules/health/service/health_service_test.go

package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// unreachableDB builds a pool pointed at a closed port. gorm's automatic ping
// is off and the pgx pool is lazy, so nothing is dialled until a check runs —
// which is exactly what is being tested.
func unreachableDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(
		postgres.Open("postgres://nobody:nobody@127.0.0.1:1/none?sslmode=disable"),
		&gorm.Config{DisableAutomaticPing: true, Logger: gormlogger.Discard},
	)
	if err != nil {
		t.Fatalf("building an unreachable gorm.DB: %v", err)
	}
	return db
}

// Liveness must not depend on anything. Checking the database here is the
// classic mistake: an outage would fail every instance's liveness probe, the
// orchestrator would restart them all, and restarting an application server
// does not fix a database.
func TestLive_ChecksNothing(t *testing.T) {
	svc := NewHealthService(unreachableDB(t))

	report := svc.Live()

	if !report.Healthy() {
		t.Fatalf("liveness failed with an unreachable database: %s", report.Status)
	}
	if len(report.Checks) != 0 {
		t.Fatalf("liveness ran %d dependency checks; it should run none", len(report.Checks))
	}
}

// Readiness is where dependencies belong: a failure takes the instance out of
// rotation without restarting it.
func TestReady_FailsWhenTheDatabaseIsUnreachable(t *testing.T) {
	svc := NewHealthService(unreachableDB(t))

	report := svc.Ready(context.Background())

	if report.Healthy() {
		t.Fatal("readiness passed with an unreachable database")
	}

	found := false
	for _, check := range report.Checks {
		if check.Name == "database" {
			found = true
			if check.Status != StatusFail {
				t.Fatalf("the database check reported %s", check.Status)
			}
		}
	}
	if !found {
		t.Fatal("readiness did not run a database check")
	}
}

// An empty registry means authorization would refuse every gated route: the
// instance is running but cannot usefully serve, which is what readiness is
// for.
func TestReady_FailsWhenTheRegistryIsCold(t *testing.T) {
	svc := NewHealthService(unreachableDB(t))

	report := svc.Ready(context.Background())

	for _, check := range report.Checks {
		if check.Name == "permissions" && check.Status != StatusFail {
			t.Fatal("a cold permission registry was reported as ready")
		}
	}
}

// **These endpoints are unauthenticated, so everything they return is public.**
// A driver error names the host, the database and often the schema.
func TestToResponse_CarriesNoDiagnosticDetail(t *testing.T) {
	svc := NewHealthService(unreachableDB(t))

	body, err := json.Marshal(ToResponse(svc.Ready(context.Background())))
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}

	rendered := strings.ToLower(string(body))
	for _, secret := range []string{
		"postgres", "connection refused", "127.0.0.1", "dial", "sslmode",
		"nobody", "error", "password",
	} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("the health response leaks %q: %s", secret, body)
		}
	}

	// It still has to be useful: the check names and their statuses.
	if !strings.Contains(rendered, "database") || !strings.Contains(rendered, "fail") {
		t.Fatalf("the response is not informative enough: %s", body)
	}
}

// A response for a nil report must still be well-formed, and must fail closed.
func TestToResponse_NilReportFailsClosed(t *testing.T) {
	response := ToResponse(nil)

	if response.Status != StatusFail.String() {
		t.Fatalf("a nil report should report failure, got %q", response.Status)
	}
}

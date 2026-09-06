// File: internal/shared/database/seed_test.go

package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// offlineDB builds a *gorm.DB that never opens a connection.
//
// The seeder runner needs a real db to derive a context-scoped session from,
// but not a reachable one: the seeders below never issue a statement. Automatic
// pinging is off and the pgx pool is lazy, so nothing is dialled.
func offlineDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(
		postgres.Open("postgres://harness:harness@127.0.0.1:1/none?sslmode=disable"),
		&gorm.Config{
			DisableAutomaticPing: true,
			Logger:               (&gormLogger{}).LogMode(gormlogger.Silent),
		},
	)
	if err != nil {
		t.Fatalf("building an offline gorm.DB: %v", err)
	}
	return db
}

// failingSeeder is a named function so the runner has a symbol to report. That
// is the whole reason it is not an inline closure.
func failingSeeder(*gorm.DB) error {
	return errors.New("permissions table is missing")
}

func TestRunSeeders_NamesTheSeederThatFailed(t *testing.T) {
	db := offlineDB(t)
	ran := 0

	seeders := []SeedFunc{
		func(*gorm.DB) error { ran++; return nil },
		failingSeeder,
		func(*gorm.DB) error { ran++; return nil },
	}

	err := RunSeeders(context.Background(), db, seeders)
	if err == nil {
		t.Fatal("want an error from the failing seeder, got nil")
	}
	if !strings.Contains(err.Error(), "failingSeeder") {
		t.Fatalf("the error should name the seeder, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "permissions table is missing") {
		t.Fatalf("the error should keep the cause, got %q", err.Error())
	}

	// The run stops at the first failure rather than pressing on into a schema
	// that is now in an unknown state.
	if ran != 1 {
		t.Fatalf("want 1 seeder run before the failure, got %d", ran)
	}
}

func TestRunSeeders_RunsEverySeeder(t *testing.T) {
	db := offlineDB(t)
	ran := 0

	seeders := []SeedFunc{
		func(*gorm.DB) error { ran++; return nil },
		func(*gorm.DB) error { ran++; return nil },
		func(*gorm.DB) error { ran++; return nil },
	}

	if err := RunSeeders(context.Background(), db, seeders); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran != 3 {
		t.Fatalf("want 3 seeders run, got %d", ran)
	}
}

// An empty value group is the normal state of a fresh template, not a problem.
func TestRunSeeders_EmptyGroupIsNotAnError(t *testing.T) {
	if err := RunSeeders(context.Background(), offlineDB(t), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A nil entry means a provider produced one, which is a wiring mistake worth a
// clear message rather than a panic several frames away.
func TestRunSeeders_RejectsANilSeeder(t *testing.T) {
	err := RunSeeders(context.Background(), offlineDB(t), []SeedFunc{nil})

	if err == nil {
		t.Fatal("want an error for a nil seeder, got nil")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Fatalf("the error should say the seeder is nil, got %q", err.Error())
	}
}

// fx cancels the start context when its timeout expires. Seeding has to notice,
// or a boot that has already been given up on keeps writing rows.
func TestRunSeeders_StopsWhenTheContextIsCancelled(t *testing.T) {
	db := offlineDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ran := 0
	err := RunSeeders(ctx, db, []SeedFunc{func(*gorm.DB) error { ran++; return nil }})

	if err == nil {
		t.Fatal("want an error for a cancelled context, got nil")
	}
	if ran != 0 {
		t.Fatalf("want no seeder to run, got %d", ran)
	}
}

func TestSeederName_FallsBackToThePosition(t *testing.T) {
	if got := seederName(2, nil); got != "#3" {
		t.Fatalf("want #3 for a nil seeder at index 2, got %q", got)
	}
}

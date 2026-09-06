// File: test/integration/database_test.go

//go:build integration

package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"go.uber.org/fx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database/migrations"
	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/test/harness"
)

// widget is a test-local entity. It exists to prove that the value groups a
// real module registers through do what the documentation says they do,
// without adding a throwaway table to the template itself.
//
// The primary key is deliberately a server-generated uuid: getting a row back
// with one is also the proof that the uuid-ossp extension was installed during
// boot.
type widget struct {
	ID   string `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Name string `gorm:"uniqueIndex;not null"`
}

func (widget) TableName() string { return "harness_widgets" }

// seededWidget is inserted by the seeder below. Keying the upsert on the unique
// column is what makes running on every boot harmless.
const seededWidget = "seeded-widget"

// seedWidgets is the idiom every real seeder should copy: one statement, no
// count-then-insert, and safe to run again.
func seedWidgets(db *gorm.DB) error {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoNothing: true,
	}).Create(&widget{Name: seededWidget}).Error
}

// widgetModule registers the entity and its seeder exactly as a feature module
// would: the model as an `any` in group:"models", the seeder as an explicit
// database.SeedFunc in group:"seeders".
func widgetModule() fx.Option {
	return fx.Module("widgets",
		fx.Provide(
			fx.Annotate(
				func() any { return &widget{} },
				fx.ResultTags(`group:"models"`),
			),
			fx.Annotate(
				func() database.SeedFunc { return seedWidgets },
				fx.ResultTags(`group:"seeders"`),
			),
		),
	)
}

// dropWidgets leaves the database as it was found, so the suite can be run
// repeatedly against the same container.
func dropWidgets(t *testing.T, db *gorm.DB) {
	t.Helper()
	t.Cleanup(func() {
		if err := db.Migrator().DropTable(&widget{}); err != nil {
			t.Errorf("dropping the test table: %v", err)
		}
	})
}

// The end-to-end claim of the whole phase: booting the real graph with a model
// and a seeder registered creates the table, installs the extension and inserts
// the row — with no code in the application that knows about either.
func TestDatabaseModule_BootCreatesRegisteredTablesAndRunsSeeders(t *testing.T) {
	var db *gorm.DB
	harness.App(t, []any{&db}, widgetModule())
	dropWidgets(t, db)

	if !db.Migrator().HasTable(&widget{}) {
		t.Fatal("the model registered in group:\"models\" did not get a table")
	}

	var count int64
	if err := db.Model(&widget{}).Where("name = ?", seededWidget).Count(&count).Error; err != nil {
		t.Fatalf("counting seeded rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("want exactly 1 seeded row, got %d", count)
	}

	// A uuid primary key came back, so uuid_generate_v4() resolved — which it
	// only does once EnsureExtensions has run.
	var seeded widget
	if err := db.Where("name = ?", seededWidget).First(&seeded).Error; err != nil {
		t.Fatalf("reading the seeded row: %v", err)
	}
	if seeded.ID == "" {
		t.Fatal("the seeded row has no server-generated uuid")
	}
}

// Seeders run on every boot. A second one must change nothing, which is the
// property that lets them run unattended.
func TestDatabaseModule_SecondBootIsIdempotent(t *testing.T) {
	var first *gorm.DB
	harness.App(t, []any{&first}, widgetModule())
	dropWidgets(t, first)

	var second *gorm.DB
	harness.App(t, []any{&second}, widgetModule())

	var count int64
	if err := second.Model(&widget{}).Where("name = ?", seededWidget).Count(&count).Error; err != nil {
		t.Fatalf("counting seeded rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("the seeder duplicated its row on the second boot: got %d", count)
	}
}

// The boot has to leave migration_records behind even with an empty catalogue,
// because that table is what makes the *next* boot able to tell what has run.
func TestDatabaseModule_BootCreatesTheMigrationRecordsTable(t *testing.T) {
	var db *gorm.DB
	harness.App(t, []any{&db}, widgetModule())
	dropWidgets(t, db)

	if !db.Migrator().HasTable(&migrations.MigrationRecord{}) {
		t.Fatal("migration_records was not created")
	}
}

// The runner itself, driven with a catalogue of its own so that the template
// need not ship a migration to have this covered.
func TestMigrations_ApplyRecordsEachVersionOnceAndOnlyOnce(t *testing.T) {
	var db *gorm.DB
	harness.App(t, []any{&db})

	applied := []int{}
	catalogue := map[int]migrations.MigrationFunc{
		1: func(tx *gorm.DB) error {
			applied = append(applied, 1)
			return tx.Exec(`CREATE TABLE IF NOT EXISTS harness_versioned (id integer PRIMARY KEY)`).Error
		},
		2: func(tx *gorm.DB) error {
			applied = append(applied, 2)
			return tx.Exec(`ALTER TABLE harness_versioned ADD COLUMN IF NOT EXISTS label text`).Error
		},
	}

	t.Cleanup(func() {
		if err := db.Exec(`DROP TABLE IF EXISTS harness_versioned`).Error; err != nil {
			t.Errorf("dropping the versioned test table: %v", err)
		}
		if err := db.Where("version IN ?", []int{1, 2}).
			Delete(&migrations.MigrationRecord{}).Error; err != nil {
			t.Errorf("clearing the test migration records: %v", err)
		}
	})

	ctx := context.Background()

	if err := migrations.Apply(ctx, db, catalogue); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("want both migrations applied, got %v", applied)
	}

	var recorded []int
	if err := db.Model(&migrations.MigrationRecord{}).
		Where("version IN ?", []int{1, 2}).
		Order("version").
		Pluck("version", &recorded).Error; err != nil {
		t.Fatalf("reading migration_records: %v", err)
	}
	if len(recorded) != 2 || recorded[0] != 1 || recorded[1] != 2 {
		t.Fatalf("want versions [1 2] recorded, got %v", recorded)
	}

	// Second run: everything is already recorded, so nothing runs again.
	if err := migrations.Apply(ctx, db, catalogue); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("a migration ran twice: %v", applied)
	}
}

// A migration and its record commit together. If the record were written
// outside the transaction, a failure would leave a version marked as applied
// that never was, and the next boot would skip it forever.
func TestMigrations_AFailedMigrationIsRolledBackAndNotRecorded(t *testing.T) {
	var db *gorm.DB
	harness.App(t, []any{&db})

	catalogue := map[int]migrations.MigrationFunc{
		1: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE harness_rollback (id integer PRIMARY KEY)`).Error; err != nil {
				return err
			}
			return tx.Exec(`THIS IS NOT SQL`).Error
		},
	}

	t.Cleanup(func() {
		if err := db.Exec(`DROP TABLE IF EXISTS harness_rollback`).Error; err != nil {
			t.Errorf("dropping the rollback test table: %v", err)
		}
	})

	err := migrations.Apply(context.Background(), db, catalogue)
	if err == nil {
		t.Fatal("want the broken migration to fail, got nil")
	}

	if db.Migrator().HasTable("harness_rollback") {
		t.Fatal("the failed migration left its table behind — it was not rolled back")
	}

	var count int64
	if err := db.Model(&migrations.MigrationRecord{}).
		Where("version = ?", 1).Count(&count).Error; err != nil {
		t.Fatalf("counting migration records: %v", err)
	}
	if count != 0 {
		t.Fatal("the failed migration was recorded as applied")
	}
}

// The guard has to fire against a real database too, and before anything is
// written — not only in a unit test.
func TestMigrations_ApplyRefusesANonContiguousCatalogue(t *testing.T) {
	var db *gorm.DB
	harness.App(t, []any{&db})

	catalogue := map[int]migrations.MigrationFunc{
		1: func(tx *gorm.DB) error { return nil },
		3: func(tx *gorm.DB) error {
			t.Error("a migration ran despite the catalogue being invalid")
			return nil
		},
	}

	err := migrations.Apply(context.Background(), db, catalogue)
	if err == nil {
		t.Fatal("want an error for the gap at version 2, got nil")
	}
}

// The claim the error model rests on: a driver error becomes a classified
// AppError with a status, and the constraint name stays out of the response.
func TestTranslateError_UniqueViolationBecomesAConflict(t *testing.T) {
	var db *gorm.DB
	harness.App(t, []any{&db}, widgetModule())
	dropWidgets(t, db)

	err := db.Create(&widget{Name: seededWidget}).Error
	if err == nil {
		t.Fatal("want a unique-constraint violation, got nil")
	}

	appErr := database.TranslateError(err)
	if appErr.Type != apperrors.TypeConflict {
		t.Fatalf("want CONFLICT, got %s (%v)", appErr.Type, err)
	}
	if appErr.ToHTTPStatus() != http.StatusConflict {
		t.Fatalf("want 409, got %d", appErr.ToHTTPStatus())
	}
	if response := appErr.Response(); response.Error == "" ||
		containsAny(response.Error, "harness_widgets", "idx_harness_widgets_name", "23505") {
		t.Fatalf("the response leaked schema detail: %q", response.Error)
	}
}

// A missing row is a NOT_FOUND, not a 500 — the other half of the same claim.
func TestTranslateError_MissingRowBecomesNotFound(t *testing.T) {
	var db *gorm.DB
	harness.App(t, []any{&db}, widgetModule())
	dropWidgets(t, db)

	var found widget
	err := db.Where("name = ?", "no-such-widget").First(&found).Error

	appErr := database.TranslateError(err)
	if appErr.Type != apperrors.TypeNotFound {
		t.Fatalf("want NOT_FOUND, got %s (%v)", appErr.Type, err)
	}
}

// Ping is what the readiness probe will call, and it must succeed against the
// pool the graph actually built.
func TestDatabase_PingSucceedsAgainstTheBootedGraph(t *testing.T) {
	var db *gorm.DB
	harness.App(t, []any{&db})

	if err := database.Ping(context.Background(), db); err != nil {
		t.Fatalf("Ping against a booted graph: %v", err)
	}
}

// The pool settings are applied rather than merely parsed — the defect this
// template exists partly to not repeat.
func TestDatabase_PoolLimitsAreApplied(t *testing.T) {
	var db *gorm.DB
	harness.App(t, []any{&db})

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("reaching the underlying sql.DB: %v", err)
	}

	if got := sqlDB.Stats().MaxOpenConnections; got <= 0 {
		t.Fatalf("MaxOpenConnections is unlimited (%d) — the pool settings were not applied", got)
	}
}

// containsAny reports whether any of needles appears in haystack. It keeps the
// leak assertion above readable when the list of things that must not appear
// grows.
func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

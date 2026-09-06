// File: internal/shared/database/migrations/runner_test.go

package migrations

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

// noop stands in for a real migration. What is being tested here is the
// numbering, not what any particular version does.
func noop(*gorm.DB) error { return nil }

// The catalogue the template ships must itself be valid, or every application
// built from it fails on its first boot.
func TestMigrationMap_ShippedCatalogueIsValid(t *testing.T) {
	if err := validateContiguous(migrationMap); err != nil {
		t.Fatalf("the shipped migration map is invalid: %v", err)
	}
}

// A project with no migrations yet is the normal starting state, not an error.
func TestValidateContiguous_AcceptsAnEmptyCatalogue(t *testing.T) {
	if err := validateContiguous(map[int]MigrationFunc{}); err != nil {
		t.Fatalf("an empty catalogue should be valid, got %v", err)
	}
	if err := validateContiguous(nil); err != nil {
		t.Fatalf("a nil catalogue should be valid, got %v", err)
	}
}

func TestValidateContiguous_AcceptsContiguousVersions(t *testing.T) {
	m := map[int]MigrationFunc{1: noop, 2: noop, 3: noop}

	if err := validateContiguous(m); err != nil {
		t.Fatalf("1..3 should be valid, got %v", err)
	}
}

// The failure this whole guard exists for: two branches both add a version, one
// renumbers badly, and the runner would otherwise walk up from 1, stop at the
// hole, and leave every later migration silently unapplied.
func TestValidateContiguous_RejectsAGapAndNamesIt(t *testing.T) {
	m := map[int]MigrationFunc{1: noop, 2: noop, 4: noop, 5: noop}

	err := validateContiguous(m)
	if err == nil {
		t.Fatal("want an error for a gap at version 3, got nil")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Fatalf("the error should name the missing version, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "contiguous") {
		t.Fatalf("the error should say what the rule is, got %q", err.Error())
	}
}

func TestValidateContiguous_RejectsMultipleGaps(t *testing.T) {
	m := map[int]MigrationFunc{1: noop, 4: noop}

	err := validateContiguous(m)
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "2") || !strings.Contains(err.Error(), "3") {
		t.Fatalf("the error should name both missing versions, got %q", err.Error())
	}
}

// Numbering from zero looks harmless and would leave version 0 permanently
// unapplied, because the runner counts from 1.
func TestValidateContiguous_RejectsVersionsBelowOne(t *testing.T) {
	cases := map[string]map[int]MigrationFunc{
		"zero":     {0: noop, 1: noop},
		"negative": {-1: noop, 1: noop},
	}

	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateContiguous(m)
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if !strings.Contains(err.Error(), "start at 1") {
				t.Fatalf("the error should say versions start at 1, got %q", err.Error())
			}
		})
	}
}

// An explicitly nil entry is the same failure as a missing one — the runner
// would call it — so it is reported the same way, before anything runs.
func TestValidateContiguous_RejectsANilMigration(t *testing.T) {
	m := map[int]MigrationFunc{1: noop, 2: nil}

	err := validateContiguous(m)
	if err == nil {
		t.Fatal("want an error for a nil migration, got nil")
	}
	if !strings.Contains(err.Error(), "v2") {
		t.Fatalf("the error should name the version, got %q", err.Error())
	}
}

// File: internal/shared/database/migrations/record.go

package migrations

import "time"

// MigrationRecord is one applied version. The table is the database's memory of
// what has already run, and it is the only reason a boot against an existing
// database is not a boot against a fresh one.
//
// Version is the primary key rather than a surrogate id with a unique index:
// the version *is* the identity, and making that the key means a second attempt
// to record the same migration is refused by the database rather than by a
// check the runner might one day forget.
//
// autoIncrement is switched off explicitly. GORM turns an integer primary key
// into a sequence by default, which would quietly renumber the versions this
// table exists to preserve.
type MigrationRecord struct {
	Version   int       `gorm:"primaryKey;autoIncrement:false"`
	AppliedAt time.Time `gorm:"not null"`
}

// TableName pins the table name, so renaming the Go type later cannot orphan
// the history.
func (MigrationRecord) TableName() string {
	return "migration_records"
}

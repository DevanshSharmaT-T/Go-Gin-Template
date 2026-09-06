// File: internal/shared/database/extension.go

package database

import (
	"context"

	"gorm.io/gorm"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// uuidOSSPStatement installs the extension that supplies uuid_generate_v4(),
// the server-side default behind every uuid primary key in the schema.
//
// Generating the identifier in the database rather than in Go means a row
// inserted by a migration, a psql session or a second service still gets a
// valid key, and there is no window in which a partially built entity has the
// zero UUID.
const uuidOSSPStatement = `CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`

// EnsureExtensions installs the PostgreSQL extensions the schema depends on.
//
// IF NOT EXISTS makes this a no-op — and, importantly, a *permitted* no-op for
// an unprivileged role — once the extension is installed. A failure here
// therefore means it is genuinely absent and the role may not create it, which
// is fatal: every table with a uuid default would fail on its first insert
// instead, one request at a time, long after the deployment looked successful.
//
// The error names the statement so it can be handed to whoever does have the
// rights, which is the fix in most managed-PostgreSQL setups.
func EnsureExtensions(ctx context.Context, db *gorm.DB) error {
	var err error = db.WithContext(ctx).Exec(uuidOSSPStatement).Error
	if err != nil {
		return errors.NewUnavailableError(
			`could not install the "uuid-ossp" extension: the database role needs `+
				`CREATE privileges once, or an administrator can run `+
				uuidOSSPStatement+`; by hand`, err)
	}

	logger.FromContext(ctx).Debug().Msg(`extension "uuid-ossp" is present`)
	return nil
}

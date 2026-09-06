// File: internal/shared/database/seed.go

package database

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// SeedFunc inserts the rows an empty database needs in order to be usable:
// roles, permissions, an administrator, lookup tables.
//
// Two rules make a seeder correct, and both are contracts this runner cannot
// enforce for you.
//
// **Seeders must be idempotent.** Every one of them runs on every boot. Key on
// a natural unique column and upsert rather than counting first:
//
//	return db.Clauses(clause.OnConflict{
//		Columns:   []clause.Column{{Name: "slug"}},
//		DoNothing: true,
//	}).Create(&roles).Error
//
// One statement, one round trip, and no race: SELECT-then-INSERT is two round
// trips and still loses to a second process starting at the same moment.
//
// **Seeders must be order-independent.** They arrive from an fx value group,
// and a value group has no defined order — it depends on provider registration,
// which changes when a module is added. A seeder that assumes another has
// already run works until the day it does not. If row B genuinely needs row A,
// insert both from the same seeder, or look A up by its natural key rather than
// by an ID you are hoping was assigned.
type SeedFunc func(*gorm.DB) error

// RunSeeders runs every registered seeder in the order the value group happens
// to supply them.
//
// Each seeder receives a context-scoped session, so a cancelled start aborts
// the work in progress instead of running to completion against a database
// nobody is waiting for any more. A failure stops the run and names the
// offending function — with an unordered group of anonymous callbacks,
// "seeder failed" alone would leave you diffing tables to find out which one.
func RunSeeders(ctx context.Context, db *gorm.DB, seeders []SeedFunc) error {
	var log *zerolog.Logger = logger.FromContext(ctx)

	if len(seeders) == 0 {
		log.Debug().Msg("no seeders registered")
		return nil
	}

	log.Info().Int("count", len(seeders)).Msg("running seeders")

	var index int
	var seed SeedFunc
	for index, seed = range seeders {
		var name string = seederName(index, seed)

		if seed == nil {
			return errors.NewInternalError(
				fmt.Sprintf("seeder %s is nil — check its fx.Provide stanza", name), nil)
		}

		var err error = ctx.Err()
		if err != nil {
			return errors.NewTimeoutError(
				fmt.Sprintf("seeding was cancelled before %s ran", name), err)
		}

		err = seed(db.WithContext(ctx))
		if err != nil {
			return errors.NewInternalError(fmt.Sprintf("seeder %s failed", name), err)
		}

		log.Debug().Str("seeder", name).Msg("seeder applied")
	}

	return nil
}

// seederName recovers a readable name for a seeder.
//
// SeedFunc is a bare function type — that is what the fx value group requires —
// so there is nowhere to hang a label. The runtime knows the symbol, though,
// and "seeds.PermissionSeeder" in an error message is the difference between a
// one-line fix and a hunt. A closure reports as something like
// "seeds.glob..func1", which still says which file to open.
func seederName(index int, seed SeedFunc) string {
	var position string = fmt.Sprintf("#%d", index+1)
	if seed == nil {
		return position
	}

	var fn *runtime.Func = runtime.FuncForPC(reflect.ValueOf(seed).Pointer())
	if fn == nil {
		return position
	}

	var name string = fn.Name()
	var lastSlash int = strings.LastIndex(name, "/")
	if lastSlash >= 0 {
		name = name[lastSlash+1:]
	}
	if name == "" {
		return position
	}
	return name
}

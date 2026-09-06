// File: internal/modules/users/seeds/admin_seeder.go

// Package seeds creates the optional first administrator, so a fresh database
// has an account that can reach the permission-gated routes.
package seeds

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	rolesdomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/roles/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/crypt"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/database"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// NewAdminSeeder creates the administrator described by the ADMIN_* variables.
//
// It carries no personal data of any kind. The name fields are derived from the
// configured username, because a template that shipped somebody's real name and
// date of birth in a seeder would put them in every clone of it.
func NewAdminSeeder(cfg *config.Config, cryptSvc *crypt.Service) database.SeedFunc {
	return func(db *gorm.DB) error {
		return seedAdmin(db, cfg, cryptSvc)
	}
}

// seedAdmin inserts the administrator, or explains why it did not.
//
// **A skip is logged, loudly.** Silently doing nothing here is
// indistinguishable from a successful boot, right up until someone discovers
// there is no way to reach an administrative route and no message anywhere
// saying why. Half-configured is worse still — it usually means a typo in one
// of three variable names — so it gets its own warning.
func seedAdmin(db *gorm.DB, cfg *config.Config, cryptSvc *crypt.Service) error {
	var log = logger.Default()

	if cfg.Admin.Partial() {
		log.Warn().Msg(
			"ADMIN_USERNAME, ADMIN_EMAIL and ADMIN_PASSWORD must all be set to seed an " +
				"administrator; some are set and some are not, so no account was created")
		return nil
	}

	if !cfg.Admin.Complete() {
		log.Info().Msg(
			"no ADMIN_* variables set, so no administrator was seeded; " +
				"set all three to create one on the next boot")
		return nil
	}

	var username string = domain.NormalizeUsername(cfg.Admin.Username)
	var email string = domain.NormalizeEmail(cfg.Admin.Email)

	// The same rules a registration goes through. An administrator created out
	// of band with an invalid username would be unable to appear in a signed
	// token payload the rest of the system can parse.
	var err error = domain.ValidateUsername(username)
	if err != nil {
		return err
	}
	err = domain.ValidateEmail(email)
	if err != nil {
		return err
	}
	err = domain.ValidatePassword(cfg.Admin.Password)
	if err != nil {
		return err
	}

	var hashed string
	hashed, err = cryptSvc.HashPassword(cfg.Admin.Password)
	if err != nil {
		return err
	}

	var now time.Time = time.Now().UTC()
	var admin domain.User = domain.User{
		Username:     username,
		Email:        email,
		PasswordHash: hashed,
		// Derived from the username: a seeder has no business inventing a
		// person's name, and these are display fields the account holder can
		// change.
		FirstName:       username,
		LastName:        "Administrator",
		RoleID:          rolesdomain.RoleIDSuperAdmin,
		Status:          domain.UserStatusActive,
		EmailVerifiedAt: &now,
	}

	// DoNothing on conflict, so a restart does not reset a password the
	// administrator has since changed — and, in particular, does not silently
	// restore the value still sitting in ADMIN_PASSWORD.
	var result *gorm.DB = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&admin)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		log.Debug().Str("username", username).Msg("administrator already exists")
		return nil
	}

	log.Warn().
		Str("username", username).
		Msg("seeded the administrator from ADMIN_* — change this password now, " +
			"and remove ADMIN_PASSWORD from the environment")
	return nil
}

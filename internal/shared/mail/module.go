// File: internal/shared/mail/module.go

package mail

import (
	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// Module provides the mailer selected by MAIL_DRIVER.
var Module = fx.Module("mail",
	fx.Provide(NewMailer),
)

// NewMailer picks a driver.
//
// The declared return type is the Mailer interface, so everything downstream
// depends on the port rather than on a driver.
//
// Only the log driver exists so far: the SMTP driver and the embedded templates
// arrive with the mail phase. Asking for smtp today therefore gets the log
// driver and a warning that says so, rather than a silent downgrade — a service
// that believes it is sending mail and is not is worse than one that says it is
// not.
func NewMailer(cfg *config.Config) Mailer {
	if cfg.Mail.UsesSMTP() {
		logger.Default().Warn().
			Str("requested_driver", cfg.Mail.Driver).
			Msg("MAIL_DRIVER=smtp is configured but the SMTP driver is not built yet; " +
				"falling back to the log driver, so no mail will be delivered")
	}
	return NewLogMailer()
}

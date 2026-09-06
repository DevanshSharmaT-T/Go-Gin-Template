// File: internal/shared/mail/module.go

package mail

import (
	"go.uber.org/fx"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// Module provides the composer and the mailer selected by MAIL_DRIVER.
var Module = fx.Module("mail",
	fx.Provide(
		NewComposer,
		NewMailer,
	),
)

// NewMailer picks a driver.
//
// The declared return type is the Mailer interface, so everything downstream
// depends on the port rather than on a driver — which is what lets the log
// driver stand in during development and in tests without a single call site
// knowing.
//
// `log` is the default because a fresh clone has no mail server, and a
// registration flow that cannot complete is a bad first impression. It is not a
// production setting: the log driver writes the whole message, single-use links
// included, to the log.
func NewMailer(cfg *config.Config) Mailer {
	if cfg.Mail.UsesSMTP() {
		logger.Default().Info().
			Str("driver", "smtp").
			Str("host", cfg.Mail.SMTPHost).
			Str("tls", cfg.Mail.SMTPTLS).
			Msg("mail will be delivered over SMTP")
		return NewSMTPMailer(cfg)
	}

	logger.Default().Warn().
		Str("driver", "log").
		Msg("MAIL_DRIVER=log: messages are written to the log and not delivered, " +
			"and that log will contain single-use verification and reset links")

	return NewLogMailer()
}

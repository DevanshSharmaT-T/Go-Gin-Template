// File: internal/shared/mail/log_mailer.go

package mail

import (
	"context"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// LogMailer renders a message to the log and sends nothing.
//
// It is the default driver (MAIL_DRIVER=log) so a fresh clone runs without a
// mail server, and it is what makes the verification and reset flows testable
// without one: the link a real user would receive by email appears on stderr.
//
// It is not a no-op writer. The body is logged in full, deliberately, because
// the only reason to run this driver is to read what would have been sent.
// **That is also the reason not to run it in production** — transactional mail
// contains single-use tokens, and this driver puts them in the log.
type LogMailer struct{}

// NewLogMailer builds the log driver.
func NewLogMailer() *LogMailer {
	return &LogMailer{}
}

// Send writes the message to the log at info level.
func (m *LogMailer) Send(ctx context.Context, message Message) error {
	var err error = message.Validate()
	if err != nil {
		return err
	}

	// The plain-text part only. The HTML body is the same content wrapped in a
	// table layout, and logging both would bury the link a developer is
	// reading this to find.
	logger.FromContext(ctx).Info().
		Str("driver", "log").
		Str("from", message.From.String()).
		Str("to", message.To).
		Str("subject", message.Subject).
		Str("body", message.Text).
		Msg("outbound email (not delivered — MAIL_DRIVER=log)")

	return nil
}

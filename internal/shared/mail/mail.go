// File: internal/shared/mail/mail.go

// Package mail delivers transactional email.
//
// It exists as a port with swappable drivers because sending mail is the part
// of a template most likely to be wrong on a fresh clone: no SMTP server, no
// credentials, and a registration flow that cannot complete because of it. The
// default driver writes the message to the log instead, so the whole
// verification and password-reset flow works end to end on a laptop with
// nothing configured, and the link is on stdout where a developer can click it.
package mail

import (
	"context"
	"strings"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// Message is one outbound email.
//
// Both bodies are optional individually but at least one must be set. A
// text-only message is deliverable everywhere; an HTML-only one renders as
// nothing in a plain-text client, which is why Send checks.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Validate checks the message is sendable before a driver tries.
//
// The address check is deliberately shallow — a non-empty value with an "@".
// Full RFC 5322 validation belongs where the address is accepted from a user,
// not here, and a mailer that re-litigates it just fails differently.
func (m Message) Validate() error {
	if strings.TrimSpace(m.To) == "" {
		return errors.NewValidationError("email message has no recipient", nil)
	}
	if !strings.Contains(m.To, "@") {
		return errors.NewValidationError("email recipient is not an address", nil)
	}
	if strings.TrimSpace(m.Subject) == "" {
		return errors.NewValidationError("email message has no subject", nil)
	}
	if strings.TrimSpace(m.Text) == "" && strings.TrimSpace(m.HTML) == "" {
		return errors.NewValidationError("email message has no body", nil)
	}
	return nil
}

// Mailer sends a message.
//
// The interface is one method on purpose. Everything a driver differs in —
// transport, authentication, retries — is behind it, and everything a caller
// cares about is in Message, so swapping the log driver for SMTP changes one
// provider and no call sites.
//
// **A failed send must not fail the operation that triggered it.** If the SMTP
// server is down, a registration should still create the account and a password
// reset should still record its token; the user can ask for another email.
// Callers log the error and carry on. The exception is a flow whose entire
// purpose is the message, where there is nothing to carry on to.
type Mailer interface {
	Send(ctx context.Context, message Message) error
}

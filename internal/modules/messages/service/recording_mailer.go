// File: internal/modules/messages/service/recording_mailer.go

package service

import (
	"context"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/messages/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/mail"
)

// RecordingMailer wraps a Mailer and writes a delivery record for every send.
//
// It is a decorator rather than a change to the mail package because recording
// is this module's concern, not the driver's — the SMTP driver should know how
// to talk to a mail server and nothing else. Every caller keeps depending on
// mail.Mailer and none of them knows this exists.
type RecordingMailer struct {
	next mail.Mailer
	log  domain.OutboundMailRepository
}

// NewRecordingMailer wraps next.
//
// **How this gets applied matters.** It is installed with fx.Decorate at the
// *root* scope — see internal/app — and not inside the messages module. A
// decoration declared inside an fx.Module applies to that module and its
// descendants only, so putting it there would leave the auth module still
// holding the undecorated mailer, and nothing would be recorded. The symptom
// would be an empty table and no error anywhere.
func NewRecordingMailer(next mail.Mailer, log domain.OutboundMailRepository) mail.Mailer {
	return &RecordingMailer{next: next, log: log}
}

// Send delivers the message and records the outcome.
//
// The record is written after the attempt, and a failure to write it does not
// turn a delivered message into a reported failure — the caller would retry and
// the recipient would get two. The reverse would be worse: a message sent and
// no record of it.
func (m *RecordingMailer) Send(ctx context.Context, message mail.Message) error {
	var sendErr error = m.next.Send(ctx, message)

	var record *domain.OutboundMail = &domain.OutboundMail{
		Recipient: message.To,
		Subject:   message.Subject,
		Status:    domain.StatusSent,
	}

	if sendErr != nil {
		record.Status = domain.StatusFailed
		// The classified message, not the driver's. This column is served by a
		// list endpoint, and driver text discloses host names and versions.
		record.Failure = failureReason(sendErr)
	}

	var recordErr error = m.log.Create(ctx, record)
	if recordErr != nil {
		logger.FromContext(ctx).Error().Err(recordErr).
			Str("to", message.To).
			Msg("could not record the outbound mail attempt")
	}

	return sendErr
}

// failureReason renders a send failure for the record.
func failureReason(err error) string {
	var appErr *errors.AppError = errors.From(err)

	// Response() is the client-safe rendering: for a 5xx it is the generic
	// message, which is what belongs in a column an administrator can list.
	var reason string = appErr.Response().Error
	if len(reason) > 500 {
		reason = reason[:500]
	}
	return reason
}

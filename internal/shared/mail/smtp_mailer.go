// File: internal/shared/mail/smtp_mailer.go

package mail

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/smtp"
	"strconv"
	"time"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
)

// SMTPMailer delivers over SMTP using the standard library.
//
// net/smtp is unmaintained in the sense that it takes no new features, but it
// is complete for this: connect, optionally upgrade to TLS, authenticate, send.
// The alternative is a dependency whose value is a nicer API around the same
// four steps, and the security-relevant decisions — whether TLS is required,
// whether credentials may cross a plaintext link — would then live in it rather
// than here where they can be read.
type SMTPMailer struct {
	host     string
	port     int
	username string
	password string
	tlsMode  string
	timeout  time.Duration
}

// NewSMTPMailer builds the driver from configuration.
func NewSMTPMailer(cfg *config.Config) *SMTPMailer {
	return &SMTPMailer{
		host:     cfg.Mail.SMTPHost,
		port:     cfg.Mail.SMTPPort,
		username: cfg.Mail.SMTPUsername,
		password: cfg.Mail.SMTPPassword,
		tlsMode:  cfg.Mail.SMTPTLS,
		timeout:  cfg.Mail.SMTPTimeout,
	}
}

// address is the host:port to dial.
func (m *SMTPMailer) address() string {
	return net.JoinHostPort(m.host, strconv.Itoa(m.port))
}

// Send delivers one message.
//
// The whole exchange is bounded by SMTP_TIMEOUT, applied as a deadline on the
// connection rather than only on the dial. A server that accepts the connection
// and then stops responding mid-conversation is a real failure mode, and
// without a deadline on the socket it hangs the request that triggered the mail
// — which, for registration, is a user waiting on a page.
func (m *SMTPMailer) Send(ctx context.Context, message Message) error {
	var payload []byte
	var err error
	payload, err = message.Encode()
	if err != nil {
		return err
	}

	var conn net.Conn
	conn, err = m.dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	var client *smtp.Client
	client, err = smtp.NewClient(conn, m.host)
	if err != nil {
		return errors.NewDependencyError("the mail server did not complete the SMTP greeting", err)
	}
	defer func() { _ = client.Close() }()

	err = m.startTLS(client)
	if err != nil {
		return err
	}

	err = m.authenticate(client)
	if err != nil {
		return err
	}

	err = client.Mail(message.From.Email)
	if err != nil {
		return errors.NewDependencyError("the mail server rejected the sender", err)
	}
	err = client.Rcpt(message.To)
	if err != nil {
		return errors.NewDependencyError("the mail server rejected the recipient", err)
	}

	var writer io.WriteCloser
	writer, err = client.Data()
	if err != nil {
		return errors.NewDependencyError("the mail server refused the message body", err)
	}

	_, err = writer.Write(payload)
	if err != nil {
		return errors.NewDependencyError("could not write the message to the mail server", err)
	}

	// Closing the writer is what commits the message; an error here means it
	// was not accepted, so it must not be swallowed by a deferred close.
	err = writer.Close()
	if err != nil {
		return errors.NewDependencyError("the mail server did not accept the message", err)
	}

	err = client.Quit()
	if err != nil {
		// The message is already committed at this point. A failed QUIT is
		// worth a log line and is not worth telling the caller the send failed,
		// which would prompt a retry and a duplicate.
		logger.FromContext(ctx).Debug().Err(err).Msg("SMTP QUIT failed after the message was accepted")
	}

	logger.FromContext(ctx).Info().
		Str("driver", "smtp").
		Str("to", message.To).
		Str("subject", message.Subject).
		Msg("email sent")

	return nil
}

// dial opens the connection, honouring both the context and SMTP_TIMEOUT.
func (m *SMTPMailer) dial(ctx context.Context) (net.Conn, error) {
	var dialer net.Dialer = net.Dialer{Timeout: m.timeout}

	var conn net.Conn
	var err error

	if m.tlsMode == config.SMTPTLSImplicit {
		// Implicit TLS: the connection is encrypted from the first byte, which
		// is what port 465 expects. MinVersion is set because Go's default for
		// a client is 1.2 and there is no reason to allow less here.
		var tlsDialer tls.Dialer = tls.Dialer{
			NetDialer: &dialer,
			Config:    &tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12},
		}
		conn, err = tlsDialer.DialContext(ctx, "tcp", m.address())
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", m.address())
	}

	if err != nil {
		return nil, errors.NewDependencyError("could not reach the mail server", err)
	}

	if m.timeout > 0 {
		// A deadline on the socket, not just on the dial, so a server that goes
		// quiet mid-exchange fails instead of hanging.
		var deadlineErr error = conn.SetDeadline(time.Now().Add(m.timeout))
		if deadlineErr != nil {
			_ = conn.Close()
			return nil, errors.NewDependencyError("could not set a deadline on the mail connection", deadlineErr)
		}
	}

	return conn, nil
}

// startTLS upgrades the connection when SMTP_TLS=starttls.
//
// **A failed upgrade is a failed send.** The tempting behaviour — try STARTTLS,
// carry on in the clear if the server does not offer it — is a downgrade an
// attacker on the path can force by stripping the advertisement, and it would
// hand over the credentials and the contents of every reset link. If starttls
// is configured, plaintext is not an acceptable outcome.
func (m *SMTPMailer) startTLS(client *smtp.Client) error {
	if m.tlsMode != config.SMTPTLSStartTLS {
		return nil
	}

	var supported bool
	supported, _ = client.Extension("STARTTLS")
	if !supported {
		return errors.NewDependencyError(
			"the mail server does not offer STARTTLS but SMTP_TLS=starttls is configured; "+
				"refusing to send in the clear", nil)
	}

	var err error = client.StartTLS(&tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return errors.NewDependencyError("could not start TLS with the mail server", err)
	}
	return nil
}

// authenticate sends the credentials, if any are configured.
//
// smtp.PlainAuth refuses to hand credentials to a server over an unencrypted
// connection unless it is localhost, and that refusal is deliberately not
// worked around here. If authentication fails with SMTP_TLS=none, the fix is to
// turn on TLS, not to silence the check.
func (m *SMTPMailer) authenticate(client *smtp.Client) error {
	if m.username == "" && m.password == "" {
		return nil
	}

	var supported bool
	supported, _ = client.Extension("AUTH")
	if !supported {
		return errors.NewDependencyError(
			"SMTP credentials are configured but the mail server does not offer AUTH", nil)
	}

	var auth smtp.Auth = smtp.PlainAuth("", m.username, m.password, m.host)
	var err error = client.Auth(auth)
	if err != nil {
		// The credentials themselves must not reach the message or the log.
		return errors.NewDependencyError("the mail server rejected the SMTP credentials", err)
	}
	return nil
}

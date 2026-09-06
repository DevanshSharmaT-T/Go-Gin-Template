// File: internal/shared/mail/smtp_mailer_test.go

package mail

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
)

// fakeSMTP is a minimal SMTP server: enough of the protocol to drive the real
// client through a real exchange.
//
// It is worth the ~70 lines. The alternative is asserting on the bytes the
// encoder produces and hoping the conversation around them is right, which
// would not have caught an out-of-order RCPT or a DATA that is never closed.
type fakeSMTP struct {
	listener net.Listener

	mu        sync.Mutex
	received  []string
	envelopes []envelope
	extension string
	closed    bool
	wg        sync.WaitGroup
}

type envelope struct {
	from string
	to   []string
	data string
}

// newFakeSMTP starts a server on a free port. extension is advertised in the
// EHLO response, so a test can offer or withhold STARTTLS and AUTH.
func newFakeSMTP(t *testing.T, extension string) *fakeSMTP {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}

	s := &fakeSMTP{listener: listener, extension: extension}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			s.handle(conn)
		}
	}()

	t.Cleanup(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		_ = listener.Close()
		s.wg.Wait()
	})

	return s
}

func (s *fakeSMTP) host() string {
	host, _, _ := net.SplitHostPort(s.listener.Addr().String())
	return host
}

func (s *fakeSMTP) port() int {
	_, port, _ := net.SplitHostPort(s.listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return n
}

// handle speaks just enough SMTP for the standard library client.
func (s *fakeSMTP) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }

	write("220 fake.example.com ESMTP")

	current := envelope{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		s.mu.Lock()
		s.received = append(s.received, line)
		s.mu.Unlock()

		verb := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(verb, "EHLO"), strings.HasPrefix(verb, "HELO"):
			if s.extension != "" {
				write("250-fake.example.com")
				write("250 " + s.extension)
			} else {
				write("250 fake.example.com")
			}

		case strings.HasPrefix(verb, "MAIL FROM:"):
			current = envelope{from: addressIn(line)}
			write("250 OK")

		case strings.HasPrefix(verb, "RCPT TO:"):
			current.to = append(current.to, addressIn(line))
			write("250 OK")

		case strings.HasPrefix(verb, "DATA"):
			write("354 End data with <CR><LF>.<CR><LF>")
			var body strings.Builder
			for {
				dataLine, dataErr := reader.ReadString('\n')
				if dataErr != nil {
					return
				}
				if dataLine == ".\r\n" {
					break
				}
				body.WriteString(dataLine)
			}
			current.data = body.String()

			s.mu.Lock()
			s.envelopes = append(s.envelopes, current)
			s.mu.Unlock()

			current = envelope{}
			write("250 OK: queued")

		case strings.HasPrefix(verb, "QUIT"):
			write("221 Bye")
			return

		case strings.HasPrefix(verb, "STARTTLS"):
			// Advertised but not implemented: a test uses this to check that
			// the client refuses rather than continuing in the clear.
			write("454 TLS not available")

		default:
			write("250 OK")
		}
	}
}

// addressIn pulls the address out of "MAIL FROM:<a@b>".
func addressIn(line string) string {
	start := strings.Index(line, "<")
	end := strings.Index(line, ">")
	if start < 0 || end < start {
		return ""
	}
	return line[start+1 : end]
}

func (s *fakeSMTP) lastEnvelope(t *testing.T) envelope {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.envelopes) == 0 {
		t.Fatal("the server received no message")
	}
	return s.envelopes[len(s.envelopes)-1]
}

func (s *fakeSMTP) mailer(tlsMode string, username string, password string) *SMTPMailer {
	return NewSMTPMailer(&config.Config{Mail: config.Mail{
		SMTPHost:     s.host(),
		SMTPPort:     s.port(),
		SMTPUsername: username,
		SMTPPassword: password,
		SMTPTLS:      tlsMode,
		SMTPTimeout:  5 * time.Second,
	}})
}

// The whole exchange, against a real client and a real socket.
func TestSMTPMailer_DeliversTheMessage(t *testing.T) {
	server := newFakeSMTP(t, "")
	mailer := server.mailer(config.SMTPTLSNone, "", "")

	message := validMessage()
	if err := mailer.Send(context.Background(), message); err != nil {
		t.Fatalf("sending: %v", err)
	}

	got := server.lastEnvelope(t)
	if got.from != "no-reply@example.com" {
		t.Fatalf("envelope sender: got %q", got.from)
	}
	if len(got.to) != 1 || got.to[0] != "someone@example.com" {
		t.Fatalf("envelope recipients: got %v", got.to)
	}
	if !strings.Contains(got.data, "Subject: ") {
		t.Fatal("the delivered message has no Subject header")
	}
	if !strings.Contains(got.data, "multipart/alternative") {
		t.Fatal("the delivered message is not multipart")
	}
}

// A message the encoder refuses must never reach the wire.
func TestSMTPMailer_DoesNotSendAnInjectedMessage(t *testing.T) {
	server := newFakeSMTP(t, "")
	mailer := server.mailer(config.SMTPTLSNone, "", "")

	message := validMessage()
	message.Subject = "Hello\r\nBcc: attacker@example.com"

	if err := mailer.Send(context.Background(), message); err == nil {
		t.Fatal("an injected message was sent")
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.envelopes) != 0 {
		t.Fatal("the server received a message that should have been refused")
	}
	if len(server.received) != 0 {
		t.Fatal("the mailer opened a connection for a message it could not encode")
	}
}

// **The downgrade test.** With SMTP_TLS=starttls, a server that does not offer
// STARTTLS must be refused. Carrying on in the clear is a downgrade anyone on
// the path can force by stripping the advertisement, and it would hand over the
// credentials and every reset link in the message.
func TestSMTPMailer_RefusesToSendInTheClearWhenSTARTTLSIsRequired(t *testing.T) {
	server := newFakeSMTP(t, "") // no STARTTLS advertised
	mailer := server.mailer(config.SMTPTLSStartTLS, "", "")

	err := mailer.Send(context.Background(), validMessage())
	if err == nil {
		t.Fatal("sent in the clear despite SMTP_TLS=starttls")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("the error should name STARTTLS, got %v", err)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.envelopes) != 0 {
		t.Fatal("a message was delivered over an unencrypted connection")
	}
}

// Advertised but broken must fail too, rather than falling back.
func TestSMTPMailer_FailsWhenTheSTARTTLSUpgradeIsRefused(t *testing.T) {
	server := newFakeSMTP(t, "STARTTLS")
	mailer := server.mailer(config.SMTPTLSStartTLS, "", "")

	if err := mailer.Send(context.Background(), validMessage()); err == nil {
		t.Fatal("a failed STARTTLS upgrade was treated as success")
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.envelopes) != 0 {
		t.Fatal("a message was delivered after the upgrade failed")
	}
}

// Configured credentials with a server that cannot take them is an error, not
// a silent unauthenticated send.
func TestSMTPMailer_FailsWhenCredentialsCannotBeUsed(t *testing.T) {
	server := newFakeSMTP(t, "") // no AUTH advertised
	mailer := server.mailer(config.SMTPTLSNone, "user", "secret")

	err := mailer.Send(context.Background(), validMessage())
	if err == nil {
		t.Fatal("credentials were configured but the message was sent unauthenticated")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("the SMTP password leaked into the error: %v", err)
	}
}

// An unreachable server is a dependency failure with a clear classification,
// not a panic or a hang.
func TestSMTPMailer_UnreachableServerIsADependencyError(t *testing.T) {
	mailer := NewSMTPMailer(&config.Config{Mail: config.Mail{
		SMTPHost: "127.0.0.1", SMTPPort: 1,
		SMTPTLS: config.SMTPTLSNone, SMTPTimeout: 2 * time.Second,
	}})

	err := mailer.Send(context.Background(), validMessage())
	if err == nil {
		t.Fatal("sending to a closed port succeeded")
	}
}

// A cancelled context must abort the dial rather than run to the timeout.
func TestSMTPMailer_HonoursACancelledContext(t *testing.T) {
	server := newFakeSMTP(t, "")
	mailer := server.mailer(config.SMTPTLSNone, "", "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := mailer.Send(ctx, validMessage()); err == nil {
		t.Fatal("sent on a cancelled context")
	}
}

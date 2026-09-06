// File: internal/shared/mail/message_test.go

package mail

import (
	"encoding/base64"
	"strings"
	"testing"

	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

func validMessage() Message {
	return Message{
		From:    Address{Name: "Example", Email: "no-reply@example.com"},
		To:      "someone@example.com",
		ToName:  "Someone",
		Subject: "A subject",
		Text:    "A body.",
		HTML:    "<p>A body.</p>",
	}
}

// The regression test for email header injection.
//
// A header ends at a CRLF, so a value carrying one continues the message with
// whatever follows. The classic route in is a user-supplied display name or a
// subject built from user input, and the payoff is an extra recipient the
// application never intended.
func TestMessage_RejectsHeaderInjection(t *testing.T) {
	injections := map[string]string{
		"crlf":         "x\r\nBcc: attacker@example.com",
		"bare lf":      "x\nBcc: attacker@example.com",
		"bare cr":      "x\rBcc: attacker@example.com",
		"nul":          "x\x00Bcc: attacker@example.com",
		"leading crlf": "\r\nBcc: attacker@example.com",
	}

	for name, payload := range injections {
		t.Run("subject/"+name, func(t *testing.T) {
			m := validMessage()
			m.Subject = payload
			assertRejected(t, m)
		})

		t.Run("recipient/"+name, func(t *testing.T) {
			m := validMessage()
			m.To = "someone@example.com" + payload
			assertRejected(t, m)
		})

		t.Run("display name/"+name, func(t *testing.T) {
			m := validMessage()
			m.ToName = payload
			assertRejected(t, m)
		})

		t.Run("sender name/"+name, func(t *testing.T) {
			m := validMessage()
			m.From.Name = payload
			assertRejected(t, m)
		})
	}
}

// assertRejected checks that a message is refused by both Validate and Encode —
// Encode calls Validate, but a future refactor could separate them, and the
// encoder is the thing that actually writes the header.
func assertRejected(t *testing.T, m Message) {
	t.Helper()

	if err := m.Validate(); err == nil {
		t.Fatal("Validate accepted a message with an injected header")
	}

	encoded, err := m.Encode()
	if err == nil {
		t.Fatalf("Encode accepted a message with an injected header:\n%s", encoded)
	}
	if !apperrors.IsType(err, apperrors.TypeValidation) {
		t.Fatalf("want VALIDATION, got %v", err)
	}
}

// mail.ParseAddress accepts "Name <a@b>", and letting that through a field the
// rest of the code treats as a bare address is how a display name arrives
// somewhere nobody expected one.
func TestMessage_RecipientMustBeABareAddress(t *testing.T) {
	for _, to := range []string{
		"Someone <someone@example.com>",
		"someone@example.com, other@example.com",
		"not an address",
		"@example.com",
		"",
	} {
		t.Run(to, func(t *testing.T) {
			m := validMessage()
			m.To = to
			if err := m.Validate(); err == nil {
				t.Fatalf("accepted %q as a recipient", to)
			}
		})
	}
}

func TestMessage_EncodeProducesAReadableMultipart(t *testing.T) {
	encoded, err := validMessage().Encode()
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	raw := string(encoded)

	for _, header := range []string{
		"From: ", "To: ", "Subject: ", "Date: ", "Message-ID: ",
		"MIME-Version: 1.0", "Content-Type: multipart/alternative",
	} {
		if !strings.Contains(raw, header) {
			t.Errorf("the encoded message has no %q header", header)
		}
	}

	// Text before HTML: a client picks the last part it understands, so the
	// order decides what a modern client shows.
	textAt := strings.Index(raw, "text/plain")
	htmlAt := strings.Index(raw, "text/html")
	if textAt < 0 || htmlAt < 0 || textAt > htmlAt {
		t.Fatalf("parts are missing or in the wrong order: text=%d html=%d", textAt, htmlAt)
	}

	// Headers and body are separated by a blank line, and every line ends CRLF.
	if strings.Contains(strings.ReplaceAll(raw, "\r\n", ""), "\n") {
		t.Fatal("the encoded message contains a bare newline")
	}

	// The bodies are base64 and decode back to what went in.
	if !strings.Contains(raw, base64.StdEncoding.EncodeToString([]byte("A body."))) {
		t.Fatal("the text part does not decode to the body")
	}
}

// No line may exceed the RFC 5322 limit, which is why the bodies are wrapped.
func TestMessage_EncodeWrapsLongBodies(t *testing.T) {
	m := validMessage()
	m.Text = strings.Repeat("a very long line with no break in it. ", 200)
	m.HTML = "<p>" + m.Text + "</p>"

	encoded, err := m.Encode()
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	for i, line := range strings.Split(string(encoded), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line %d is %d octets, past the RFC 5322 limit", i, len(line))
		}
	}
}

// A message with no sender cannot be encoded: the composer sets it, and a
// caller building a Message by hand should hear about it rather than produce
// something the server rejects.
func TestMessage_EncodeRequiresASender(t *testing.T) {
	m := validMessage()
	m.From = Address{}

	if _, err := m.Encode(); err == nil {
		t.Fatal("encoded a message with no sender")
	}
}

func TestAddress_EncodesNonASCIIDisplayNames(t *testing.T) {
	plain := Address{Name: "Support", Email: "support@example.com"}
	if got := plain.String(); got != "Support <support@example.com>" {
		t.Fatalf("plain name: got %q", got)
	}

	// A non-ASCII name has to be encoded, or it is not a valid header.
	unicode := Address{Name: "Käse Büro", Email: "support@example.com"}
	got := unicode.String()
	if !strings.HasPrefix(got, "=?utf-8?") {
		t.Fatalf("a non-ASCII display name was not encoded: %q", got)
	}

	bare := Address{Email: "support@example.com"}
	if got := bare.String(); got != "support@example.com" {
		t.Fatalf("bare address: got %q", got)
	}
}

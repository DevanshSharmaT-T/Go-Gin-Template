// File: internal/shared/mail/message.go

package mail

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// Address is a recipient or sender, with an optional display name.
type Address struct {
	Name  string
	Email string
}

// String renders the address as a header value, encoding the display name if it
// is not plain ASCII.
func (a Address) String() string {
	if a.Name == "" {
		return a.Email
	}
	return mime.QEncoding.Encode("utf-8", a.Name) + " <" + a.Email + ">"
}

// maxLineOctets is the base64 line length. RFC 5322 limits a line to 998
// octets; 76 is the conventional MIME width and is what every client expects.
const maxLineOctets = 76

// Encode renders the message as RFC 5322 bytes, ready for an SMTP DATA command.
//
// The body is always multipart/alternative with a plain-text part first and an
// HTML part second, which is the order the standard defines: a client picks the
// last part it understands, so text-only clients get the text and everything
// else gets the HTML.
//
// Both parts are base64-encoded. That sidesteps every line-length and
// bare-newline question in one step — a body containing a very long URL, which
// every one of these messages does, would otherwise need quoted-printable
// soft-wrapping to stay inside the line limit.
func (m Message) Encode() ([]byte, error) {
	var err error = m.Validate()
	if err != nil {
		return nil, err
	}

	var boundary string
	boundary, err = randomBoundary()
	if err != nil {
		return nil, err
	}

	var from Address = m.From
	if from.Email == "" {
		return nil, errors.NewInternalError("email message has no sender", nil)
	}

	var messageID string
	messageID, err = randomMessageID(from.Email)
	if err != nil {
		return nil, err
	}

	var b strings.Builder

	writeHeader(&b, "From", from.String())
	writeHeader(&b, "To", Address{Name: m.ToName, Email: m.To}.String())
	writeHeader(&b, "Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	writeHeader(&b, "Date", time.Now().UTC().Format(time.RFC1123Z))
	writeHeader(&b, "Message-ID", messageID)
	writeHeader(&b, "MIME-Version", "1.0")

	// Transactional mail should not be filed as a promotion or auto-replied to.
	writeHeader(&b, "Auto-Submitted", "auto-generated")
	writeHeader(&b, "X-Auto-Response-Suppress", "All")

	writeHeader(&b, "Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")

	var text string = m.Text
	if strings.TrimSpace(text) == "" {
		text = "This message requires an email client that can display HTML."
	}

	writePart(&b, boundary, "text/plain; charset=utf-8", text)
	if strings.TrimSpace(m.HTML) != "" {
		writePart(&b, boundary, "text/html; charset=utf-8", m.HTML)
	}
	b.WriteString("--" + boundary + "--\r\n")

	return []byte(b.String()), nil
}

// writeHeader writes one header line.
func writeHeader(b *strings.Builder, name string, value string) {
	b.WriteString(name)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\r\n")
}

// writePart writes one MIME part with a base64 body.
func writePart(b *strings.Builder, boundary string, contentType string, body string) {
	b.WriteString("--" + boundary + "\r\n")
	writeHeader(b, "Content-Type", contentType)
	writeHeader(b, "Content-Transfer-Encoding", "base64")
	b.WriteString("\r\n")
	b.WriteString(wrapBase64(body))
}

// wrapBase64 encodes a body and folds it to the MIME line width.
func wrapBase64(body string) string {
	var encoded string = base64.StdEncoding.EncodeToString([]byte(body))

	var b strings.Builder
	var i int
	for i = 0; i < len(encoded); i += maxLineOctets {
		var end int = i + maxLineOctets
		if end > len(encoded) {
			end = len(encoded)
		}
		b.WriteString(encoded[i:end])
		b.WriteString("\r\n")
	}
	return b.String()
}

// randomBoundary returns a MIME boundary that cannot appear in a base64 body.
func randomBoundary() (string, error) {
	var buf []byte = make([]byte, 24)
	var _, err = rand.Read(buf)
	if err != nil {
		return "", errors.NewInternalError("could not generate a MIME boundary", err)
	}
	return "b_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

// randomMessageID returns a globally unique Message-ID.
func randomMessageID(from string) (string, error) {
	var buf []byte = make([]byte, 16)
	var _, err = rand.Read(buf)
	if err != nil {
		return "", errors.NewInternalError("could not generate a Message-ID", err)
	}

	var domain string = "localhost"
	var at int = strings.LastIndex(from, "@")
	if at >= 0 && at+1 < len(from) {
		domain = from[at+1:]
	}

	return fmt.Sprintf("<%s@%s>", base64.RawURLEncoding.EncodeToString(buf), domain), nil
}

// headerUnsafe reports whether a value could break out of its header.
//
// **This is the check that stops email header injection.** A header ends at a
// CRLF, so a value carrying one continues the message with whatever follows —
// a subject of "Hello\r\nBcc: everyone@example.com" adds a recipient the
// application never intended, and the classic version of this bug is a
// user-supplied display name going straight into a From or Subject.
//
// Bare CR and bare LF are rejected as well as the pair: SMTP servers and MIME
// parsers differ on how they normalise a lone newline, and a value that is
// harmless to one and a header separator to another is exactly the sort of
// disagreement to refuse rather than reason about. NUL goes too, since it
// truncates in any C-based server on the path.
func headerUnsafe(value string) bool {
	return strings.ContainsAny(value, "\r\n\x00")
}

// validAddress reports whether value parses as a single email address.
func validAddress(value string) bool {
	if headerUnsafe(value) {
		return false
	}
	var parsed, err = mail.ParseAddress(value)
	return err == nil && parsed.Address == value
}

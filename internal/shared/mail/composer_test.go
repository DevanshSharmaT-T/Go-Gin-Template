// File: internal/shared/mail/composer_test.go

package mail

import (
	"strings"
	"testing"
	"time"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
)

func testComposer(t *testing.T) *Composer {
	t.Helper()

	c, err := NewComposer(&config.Config{
		App:  config.App{Name: "Example App"},
		Mail: config.Mail{FromAddress: "no-reply@example.com", FromName: "Example App"},
	})
	if err != nil {
		t.Fatalf("building the composer: %v", err)
	}
	return c
}

// Parsing at startup means a broken template is a boot failure rather than an
// error nobody sees until somebody is locked out and asks for a reset.
func TestNewComposer_ParsesEveryTemplateAtStartup(t *testing.T) {
	c := testComposer(t)

	for _, name := range templateNames() {
		if c.html[name] == nil {
			t.Errorf("no HTML template parsed for %q", name)
		}
		if c.text[name] == nil {
			t.Errorf("no text template parsed for %q", name)
		}
	}
}

// Every template must render both parts, with the link in each. An HTML-only
// message is blank in a text client; a text-only one looks broken beside
// everything else in an inbox.
func TestComposer_RendersBothPartsWithTheLink(t *testing.T) {
	c := testComposer(t)
	link := "https://app.example.com/verify-email?token=abc123"

	cases := map[string]func() (Message, error){
		"verify": func() (Message, error) {
			return c.VerifyEmail("someone@example.com", "Ada Lovelace", link, 24*time.Hour)
		},
		"reset": func() (Message, error) {
			return c.ResetPassword("someone@example.com", "Ada Lovelace", link, time.Hour)
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := build()
			if err != nil {
				t.Fatalf("composing: %v", err)
			}

			if strings.TrimSpace(m.Text) == "" {
				t.Fatal("no text part")
			}
			if strings.TrimSpace(m.HTML) == "" {
				t.Fatal("no HTML part")
			}
			if !strings.Contains(m.Text, link) {
				t.Fatalf("the text part does not carry the link:\n%s", m.Text)
			}
			if !strings.Contains(m.HTML, link) {
				t.Fatalf("the HTML part does not carry the link")
			}
			if !strings.Contains(m.Text, "Ada Lovelace") {
				t.Fatal("the text part does not greet the recipient")
			}
			if m.From.Email != "no-reply@example.com" {
				t.Fatalf("sender: got %q", m.From.Email)
			}
			if !strings.Contains(m.Subject, "Example App") {
				t.Fatalf("subject does not name the app: %q", m.Subject)
			}
		})
	}
}

// The two templates must not have overwritten each other during parsing, which
// is what happens if they share one template set — they both define "content".
func TestComposer_TemplatesDoNotOverwriteEachOther(t *testing.T) {
	c := testComposer(t)
	link := "https://app.example.com/x?token=abc"

	verify, err := c.VerifyEmail("someone@example.com", "Ada", link, time.Hour)
	if err != nil {
		t.Fatalf("composing: %v", err)
	}
	reset, err := c.ResetPassword("someone@example.com", "Ada", link, time.Hour)
	if err != nil {
		t.Fatalf("composing: %v", err)
	}

	if verify.Text == reset.Text || verify.HTML == reset.HTML {
		t.Fatal("both templates rendered the same body; one overwrote the other")
	}
	if !strings.Contains(strings.ToLower(verify.Text), "confirm") {
		t.Fatalf("the verification email is not about confirming:\n%s", verify.Text)
	}
	if !strings.Contains(strings.ToLower(reset.Text), "password") {
		t.Fatalf("the reset email is not about a password:\n%s", reset.Text)
	}
}

// html/template escapes contextually, so a display name carrying markup is
// inert in the HTML part.
func TestComposer_EscapesTheRecipientNameInHTML(t *testing.T) {
	c := testComposer(t)

	m, err := c.VerifyEmail("someone@example.com",
		`<script>alert(1)</script>`,
		"https://app.example.com/verify?token=abc", time.Hour)
	if err != nil {
		t.Fatalf("composing: %v", err)
	}

	if strings.Contains(m.HTML, "<script>") {
		t.Fatalf("a script tag survived into the HTML part:\n%s", m.HTML)
	}
	if !strings.Contains(m.HTML, "&lt;script&gt;") {
		t.Fatal("the name was dropped rather than escaped")
	}
}

// A link is put in front of a person and clicked. javascript: and data: URLs
// have no business in one, and html/template's neutralisation would not cover
// the text part, where nothing is escaped.
func TestComposer_RefusesANonHTTPActionLink(t *testing.T) {
	c := testComposer(t)

	for _, link := range []string{
		"javascript:alert(1)",
		"data:text/html;base64,PHNjcmlwdD4=",
		"file:///etc/passwd",
		"/relative/path",
		"",
	} {
		t.Run(link, func(t *testing.T) {
			if _, err := c.VerifyEmail("someone@example.com", "Ada", link, time.Hour); err == nil {
				t.Fatalf("accepted %q as an action link", link)
			}
		})
	}
}

// A misconfigured sender must fail at startup rather than on the first send.
func TestNewComposer_RejectsAnUnusableSender(t *testing.T) {
	cases := map[string]config.Mail{
		"no address":      {FromAddress: "", FromName: "Example"},
		"not an address":  {FromAddress: "not-an-address", FromName: "Example"},
		"injected name":   {FromAddress: "no-reply@example.com", FromName: "x\r\nBcc: a@b.c"},
		"name in address": {FromAddress: "Example <no-reply@example.com>", FromName: ""},
	}

	for name, mailCfg := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewComposer(&config.Config{App: config.App{Name: "Example"}, Mail: mailCfg})
			if err == nil {
				t.Fatal("accepted an unusable sender")
			}
		})
	}
}

// The TTL is rendered for a person, not as a Go duration.
func TestHumanDuration(t *testing.T) {
	cases := map[time.Duration]string{
		24 * time.Hour:   "24 hours",
		time.Hour:        "1 hour",
		48 * time.Hour:   "2 days",
		72 * time.Hour:   "3 days",
		30 * time.Minute: "30 minutes",
		time.Minute:      "1 minute",
		10 * time.Second: "a few moments",
	}

	for d, want := range cases {
		if got := humanDuration(d); got != want {
			t.Errorf("humanDuration(%s) = %q, want %q", d, got, want)
		}
	}
}

// An empty name must not produce "Hello ,".
func TestDisplayName_FallsBack(t *testing.T) {
	if got := displayName("  "); got != "there" {
		t.Fatalf("want a fallback greeting, got %q", got)
	}
	if got := displayName(" Ada "); got != "Ada" {
		t.Fatalf("want the trimmed name, got %q", got)
	}
}

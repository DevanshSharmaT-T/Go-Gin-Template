// File: internal/shared/mail/composer.go

package mail

import (
	"embed"
	htmltemplate "html/template"
	"strconv"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// templateFS carries the templates into the binary.
//
// //go:embed is what makes the compiled server a single file with no runtime
// dependency on a templates directory — which matters for a scratch container
// image, and matters just as much for the test harness, which runs from an
// empty temporary directory and would otherwise find nothing.
//
//go:embed templates/*.html templates/*.txt
var templateFS embed.FS

// Template names. Each one needs a .html and a .txt file: a message with only
// an HTML part renders as blank in a text-only client, and one with only text
// looks broken next to everything else in an inbox.
const (
	TemplateVerifyEmail   = "verify_email"
	TemplateResetPassword = "reset_password"
)

// templateNames is every template the composer knows, so a missing file is a
// startup failure rather than a failure the first time that particular email is
// sent — which, for password reset, could be weeks later.
func templateNames() []string {
	return []string{TemplateVerifyEmail, TemplateResetPassword}
}

// templateData is what every template can reference.
//
// The HTML side is rendered with html/template, so every field is contextually
// escaped: a display name containing markup is inert, and the URL goes through
// the same escaping and URL filtering as any other value in an href.
//
// **ActionURL is a plain string on purpose.** html/template has a URL type that
// marks a value as pre-sanitised, and using it here would switch off exactly
// the filtering that makes a link in an email safe. Leaving it a string costs
// nothing — an `&` renders as `&amp;`, which is correct HTML and which browsers
// decode — and means the escaper still refuses a javascript: URL if one ever
// reaches this far. [isHTTPURL] is the belt to that pair of braces, and it also
// covers the text part, where nothing is escaped at all.
type templateData struct {
	AppName     string
	Subject     string
	Name        string
	ActionURL   string
	ActionLabel string
	ExpiresIn   string
}

// Composer turns application events into rendered messages.
//
// It exists so the wording of an email is not embedded in the service that
// triggers it. The auth service decides that a verification link should be
// sent; what that message says, and how it looks, is this package's business.
type Composer struct {
	from    Address
	appName string

	// One parsed set per template, keyed by name. They cannot share a set:
	// every template file defines a block called "content", so parsing them
	// together would leave the last one having silently overwritten the rest.
	html map[string]*htmltemplate.Template
	text map[string]*texttemplate.Template
}

// NewComposer parses the embedded templates.
//
// Parsing happens once, at startup, and a broken template is a boot failure.
// The alternative — parsing on each send — turns a typo in the password-reset
// template into an error nobody sees until somebody is locked out.
func NewComposer(cfg *config.Config) (*Composer, error) {
	var c *Composer = &Composer{
		from:    Address{Name: cfg.Mail.FromName, Email: cfg.Mail.FromAddress},
		appName: cfg.App.Name,
		html:    map[string]*htmltemplate.Template{},
		text:    map[string]*texttemplate.Template{},
	}

	if headerUnsafe(c.from.Name) || !validAddress(c.from.Email) {
		return nil, errors.NewValidationError(
			"MAIL_FROM_ADDRESS and MAIL_FROM_NAME must be a usable sender", nil)
	}

	var name string
	for _, name = range templateNames() {
		var err error = c.parse(name)
		if err != nil {
			return nil, err
		}
	}

	return c, nil
}

// parse loads one template pair into its own set.
func (c *Composer) parse(name string) error {
	var html *htmltemplate.Template
	var err error
	html, err = htmltemplate.New(name).ParseFS(templateFS,
		"templates/layout.html", "templates/"+name+".html")
	if err != nil {
		return errors.NewInternalError("could not parse the "+name+" HTML template", err)
	}
	c.html[name] = html

	var text *texttemplate.Template
	text, err = texttemplate.New(name).ParseFS(templateFS, "templates/"+name+".txt")
	if err != nil {
		return errors.NewInternalError("could not parse the "+name+" text template", err)
	}
	c.text[name] = text

	return nil
}

// VerifyEmail builds the address-confirmation message.
func (c *Composer) VerifyEmail(to string, name string, link string, ttl time.Duration) (Message, error) {
	return c.compose(TemplateVerifyEmail, to, name,
		"Confirm your email address", "Confirm my email address", link, ttl)
}

// ResetPassword builds the password-reset message.
func (c *Composer) ResetPassword(to string, name string, link string, ttl time.Duration) (Message, error) {
	return c.compose(TemplateResetPassword, to, name,
		"Reset your password", "Choose a new password", link, ttl)
}

// compose renders both bodies and assembles the message.
func (c *Composer) compose(
	template string,
	to string,
	name string,
	subject string,
	actionLabel string,
	link string,
	ttl time.Duration,
) (Message, error) {
	if !isHTTPURL(link) {
		// html/template would neutralise a javascript: or data: URL in the
		// href, which is good — but it would still appear verbatim in the text
		// part, where nothing escapes it. Refusing here covers both.
		return Message{}, errors.NewValidationError(
			"an email action link must be an http or https URL", nil)
	}

	var subjectLine string = subject + " · " + c.appName

	var data templateData = templateData{
		AppName:     c.appName,
		Subject:     subjectLine,
		Name:        displayName(name),
		ActionURL:   link,
		ActionLabel: actionLabel,
		ExpiresIn:   humanDuration(ttl),
	}

	var html string
	var text string
	var err error

	html, err = c.renderHTML(template, data)
	if err != nil {
		return Message{}, err
	}
	text, err = c.renderText(template, data)
	if err != nil {
		return Message{}, err
	}

	var message Message = Message{
		From:    c.from,
		To:      to,
		ToName:  data.Name,
		Subject: subjectLine,
		Text:    text,
		HTML:    html,
	}

	err = message.Validate()
	if err != nil {
		return Message{}, err
	}
	return message, nil
}

// renderHTML runs the html/template set.
func (c *Composer) renderHTML(name string, data templateData) (string, error) {
	var set *htmltemplate.Template
	var known bool
	set, known = c.html[name]
	if !known {
		return "", errors.NewInternalError("no HTML template named "+name, nil)
	}

	var b strings.Builder
	var err error = set.ExecuteTemplate(&b, "layout", data)
	if err != nil {
		return "", errors.NewInternalError("could not render the "+name+" HTML template", err)
	}
	return b.String(), nil
}

// renderText runs the text/template set.
func (c *Composer) renderText(name string, data templateData) (string, error) {
	var set *texttemplate.Template
	var known bool
	set, known = c.text[name]
	if !known {
		return "", errors.NewInternalError("no text template named "+name, nil)
	}

	var b strings.Builder
	var err error = set.ExecuteTemplate(&b, "content", data)
	if err != nil {
		return "", errors.NewInternalError("could not render the "+name+" text template", err)
	}
	return strings.TrimLeft(b.String(), "\n"), nil
}

// isHTTPURL reports whether a link is safe to put in front of a person.
func isHTTPURL(link string) bool {
	return strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://")
}

// displayName falls back to a neutral greeting rather than "Hello ,".
func displayName(name string) string {
	var trimmed string = strings.TrimSpace(name)
	if trimmed == "" {
		return "there"
	}
	return trimmed
}

// humanDuration renders a TTL the way a person would say it, so the email reads
// "expires in 24 hours" rather than "expires in 24h0m0s".
func humanDuration(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return plural(int(d.Hours())/24, "day")
	case d >= time.Hour:
		return plural(int(d.Hours()), "hour")
	case d >= time.Minute:
		return plural(int(d.Minutes()), "minute")
	default:
		return "a few moments"
	}
}

func plural(n int, unit string) string {
	var value string = strconv.Itoa(n) + " " + unit
	if n != 1 {
		value += "s"
	}
	return value
}

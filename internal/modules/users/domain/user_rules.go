// File: internal/modules/users/domain/user_rules.go

package domain

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// Password length bounds.
//
// The floor is a policy choice; the ceiling is not. bcrypt silently truncates
// its input at 72 bytes, so a longer password is accepted and then only the
// first 72 bytes are ever checked — two different long passwords sharing a
// prefix would both open the account. Rejecting is honest; truncating is not.
const (
	MinPasswordLength = 8
	MaxPasswordBytes  = 72
)

// Username bounds and shape.
const (
	MinUsernameLength = 3
	MaxUsernameLength = 50
)

// usernamePattern is an allow-list, not a deny-list.
//
// The characters permitted here are the ones that survive being placed in a
// URL, a JSON string and a signed token payload without needing to be escaped
// differently in each. Anything outside it is refused rather than sanitised —
// stripping characters silently maps two requested usernames onto one account.
var usernamePattern *regexp.Regexp = regexp.MustCompile(`^[a-z0-9._-]+$`)

// emailShapedPattern matches anything that looks like an address.
//
// Usernames that look like email addresses are rejected because the login form
// accepts either: allow "a@b.com" as a username and whoever registers it can
// intercept logins intended for the account with that address.
var emailShapedPattern *regexp.Regexp = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// emailPattern is a deliberately loose structural check.
//
// Fully validating an address against RFC 5322 is famously not worth it, and a
// strict regex mostly succeeds at rejecting valid addresses. The only check
// that establishes an address exists is sending mail to it, which is what
// verification is for; this just catches obvious nonsense early.
var emailPattern *regexp.Regexp = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]{2,}$`)

// ValidateUsername enforces the allow-list. The argument is expected to be
// normalised already.
func ValidateUsername(username string) error {
	var length int = utf8.RuneCountInString(username)
	if length < MinUsernameLength || length > MaxUsernameLength {
		return errors.NewValidationError(
			"username must be between 3 and 50 characters", nil).
			WithDetail("username", "must be between 3 and 50 characters")
	}
	if emailShapedPattern.MatchString(username) {
		return errors.NewValidationError(
			"username cannot look like an email address", nil).
			WithDetail("username", "cannot look like an email address")
	}
	if !usernamePattern.MatchString(username) {
		return errors.NewValidationError(
			"username may contain only letters, digits, dot, underscore and hyphen", nil).
			WithDetail("username", "may contain only letters, digits, dot, underscore and hyphen")
	}
	return nil
}

// ValidateEmail applies the structural check.
func ValidateEmail(email string) error {
	if !emailPattern.MatchString(email) {
		return errors.NewValidationError("email address is not valid", nil).
			WithDetail("email", "is not a valid email address")
	}
	if utf8.RuneCountInString(email) > 255 {
		return errors.NewValidationError("email address is too long", nil).
			WithDetail("email", "is too long")
	}
	return nil
}

// ValidatePassword enforces the length bounds.
//
// Length is the only rule. Composition requirements — one digit, one symbol —
// measurably push people towards "Password1!" and its cousins, which is why
// NIST dropped them; length and a check against known-breached passwords are
// what actually help, and the second belongs behind a service call rather than
// in a template.
func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return errors.NewValidationError("password must be at least 8 characters", nil).
			WithDetail("password", "must be at least 8 characters")
	}
	if len(password) > MaxPasswordBytes {
		return errors.NewValidationError(
			"password must be at most 72 bytes", nil).
			WithDetail("password", "must be at most 72 bytes, because bcrypt ignores anything beyond that")
	}
	if strings.TrimSpace(password) == "" {
		return errors.NewValidationError("password cannot be blank", nil).
			WithDetail("password", "cannot be blank")
	}
	return nil
}

// ValidateName checks a first or last name.
func ValidateName(field string, value string) error {
	var trimmed string = strings.TrimSpace(value)
	if trimmed == "" {
		return errors.NewValidationError(field+" is required", nil).
			WithDetail(field, "is required")
	}
	if utf8.RuneCountInString(trimmed) > 100 {
		return errors.NewValidationError(field+" is too long", nil).
			WithDetail(field, "must be at most 100 characters")
	}
	return nil
}

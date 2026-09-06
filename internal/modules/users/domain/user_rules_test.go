// File: internal/modules/users/domain/user_rules_test.go

package domain

import (
	"strings"
	"testing"

	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

func TestValidateUsername_AcceptsTheAllowList(t *testing.T) {
	for _, username := range []string{"alice", "bob.smith", "a_b-c", "user123", strings.Repeat("a", 50)} {
		t.Run(username, func(t *testing.T) {
			if err := ValidateUsername(username); err != nil {
				t.Fatalf("want %q accepted, got %v", username, err)
			}
		})
	}
}

// The allow-list exists because these characters end up in URLs, JSON and
// signed token payloads, each of which escapes differently.
func TestValidateUsername_RejectsEverythingElse(t *testing.T) {
	cases := map[string]string{
		"too short":        "ab",
		"too long":         strings.Repeat("a", 51),
		"space":            "alice smith",
		"slash":            "alice/../admin",
		"pipe":             "alice|role=admin",
		"colon":            "alice:admin",
		"percent":          "alice%20",
		"null byte":        "alice\x00",
		"newline":          "alice\nadmin",
		"angle brackets":   "<script>",
		"non-ascii":        "alicé",
		"empty":            "",
		"email shaped":     "alice@example.com",
		"email shaped alt": "a@b.co",
	}

	for name, username := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidateUsername(username)
			if err == nil {
				t.Fatalf("want %q rejected, got nil", username)
			}
			if !apperrors.IsType(err, apperrors.TypeValidation) {
				t.Fatalf("want VALIDATION, got %v", err)
			}
		})
	}
}

// A username that looks like an address is refused because the login field
// accepts either: allowing it lets someone register "victim@example.com" as a
// username and intercept logins meant for that address.
func TestValidateUsername_RejectsEmailShapedNames(t *testing.T) {
	err := ValidateUsername("victim@example.com")
	if err == nil {
		t.Fatal("an email-shaped username was accepted")
	}
	if !strings.Contains(err.Error(), "email") {
		t.Fatalf("the reason should mention email, got %q", err.Error())
	}
}

func TestValidatePassword_EnforcesBothBounds(t *testing.T) {
	if err := ValidatePassword(strings.Repeat("a", MinPasswordLength)); err != nil {
		t.Fatalf("a password at the minimum length was rejected: %v", err)
	}
	if err := ValidatePassword(strings.Repeat("a", MinPasswordLength-1)); err == nil {
		t.Fatal("a short password was accepted")
	}

	// The upper bound is not a policy choice: bcrypt ignores everything past 72
	// bytes, so accepting a longer password would only ever check its prefix.
	if err := ValidatePassword(strings.Repeat("a", MaxPasswordBytes)); err != nil {
		t.Fatalf("a password at the byte limit was rejected: %v", err)
	}
	if err := ValidatePassword(strings.Repeat("a", MaxPasswordBytes+1)); err == nil {
		t.Fatal("a password past bcrypt's 72-byte limit was accepted; it would be truncated")
	}

	// The limit is bytes, not runes — a multi-byte password can pass a rune
	// count and still be truncated.
	if err := ValidatePassword(strings.Repeat("é", 40)); err == nil {
		t.Fatal("an 80-byte password was accepted because it is only 40 runes")
	}
}

func TestValidateEmail_RejectsObviousNonsense(t *testing.T) {
	for _, email := range []string{"", "alice", "alice@", "@example.com", "alice@example", "a b@example.com"} {
		t.Run(email, func(t *testing.T) {
			if err := ValidateEmail(email); err == nil {
				t.Fatalf("want %q rejected, got nil", email)
			}
		})
	}

	for _, email := range []string{"alice@example.com", "alice+tag@sub.example.co.uk"} {
		t.Run(email, func(t *testing.T) {
			if err := ValidateEmail(email); err != nil {
				t.Fatalf("want %q accepted, got %v", email, err)
			}
		})
	}
}

// Normalising on the way in is what makes the unique index mean what everyone
// assumes it means.
func TestNormalizeEmail_FoldsCaseAndTrims(t *testing.T) {
	for _, input := range []string{"Alice@Example.COM", "  alice@example.com  ", "ALICE@EXAMPLE.COM"} {
		if got := NormalizeEmail(input); got != "alice@example.com" {
			t.Fatalf("NormalizeEmail(%q) = %q", input, got)
		}
	}
}

func TestUserStatus_ValidRejectsArbitraryStrings(t *testing.T) {
	for _, status := range []UserStatus{UserStatusActive, UserStatusInactive, UserStatusSuspended} {
		if !status.Valid() {
			t.Fatalf("%q should be valid", status)
		}
	}
	for _, status := range []UserStatus{"", "admin", "ACTIVE", "deleted"} {
		if status.Valid() {
			t.Fatalf("%q should not be valid", status)
		}
	}
}

// Registration assigns this constant; a request cannot influence it. If this
// ever stops being a constant, the privilege-escalation hole is back.
func TestDefaultRoleID_IsAConstant(t *testing.T) {
	if DefaultRoleID <= 0 {
		t.Fatalf("DefaultRoleID must be a positive role id, got %d", DefaultRoleID)
	}
}

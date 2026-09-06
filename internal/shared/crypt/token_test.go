// File: internal/shared/crypt/token_test.go

package crypt

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// testService builds a Service without going through configuration loading.
func testService(t *testing.T) *Service {
	t.Helper()
	return &Service{
		bcryptCost:  4, // the minimum bcrypt accepts; these tests are not measuring work factor
		tokenSecret: []byte("a-test-token-signing-key-of-sufficient-length"),
	}
}

func TestIssueToken_RoundTrips(t *testing.T) {
	s := testService(t)
	subject := uuid.New()

	token, payload, err := s.IssueToken(subject, PurposePasswordReset, time.Hour)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	got, err := s.VerifyToken(token, PurposePasswordReset)
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}

	if got.Subject != subject {
		t.Fatalf("subject: want %s, got %s", subject, got.Subject)
	}
	if got.Purpose != PurposePasswordReset {
		t.Fatalf("purpose: want %s, got %s", PurposePasswordReset, got.Purpose)
	}
	if got.Nonce != payload.Nonce {
		t.Fatal("the nonce did not survive the round trip")
	}
}

// Two tokens for the same subject and purpose must differ, or the fingerprint
// that makes them single-use would collide and the second issue would be
// refused by the unique index.
func TestIssueToken_IsUniquePerIssue(t *testing.T) {
	s := testService(t)
	subject := uuid.New()

	first, _, err := s.IssueToken(subject, PurposeEmailVerification, time.Hour)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}
	second, _, err := s.IssueToken(subject, PurposeEmailVerification, time.Hour)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	if first == second {
		t.Fatal("two issues produced the same token")
	}
}

// The regression test for the delimiter-injection class of bug.
//
// The vulnerable design serialises the payload as `key=value|key=value` and
// signs the string. A value containing the delimiter then smuggles extra fields
// into a payload the server itself signs: the signature verifies, and the parse
// is attacker-controlled. JSON has one encoding of any string, so the same
// input is carried as data rather than as structure.
func TestIssueToken_DelimiterInPayloadCannotForgeFields(t *testing.T) {
	s := testService(t)

	// A subject is a UUID and cannot carry a delimiter, so the injection is
	// staged where a value *is* attacker-influenced: the encoded payload is
	// rebuilt with a nonce full of separators and re-signed, exactly as a
	// vulnerable implementation would have done.
	hostile := TokenPayload{
		Subject:   uuid.New(),
		Purpose:   PurposeEmailVerification,
		Nonce:     `x|purpose=password_reset|sub=00000000-0000-0000-0000-000000000000`,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}

	encoded, err := json.Marshal(hostile)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(encoded) +
		tokenSeparator +
		base64.RawURLEncoding.EncodeToString(s.sign(encoded))

	// The token is validly signed, so it verifies — but only as what it says it
	// is. The delimiters are inert data inside the nonce.
	got, err := s.VerifyToken(token, PurposeEmailVerification)
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if got.Purpose != PurposeEmailVerification {
		t.Fatalf("the payload was reinterpreted: purpose is %s", got.Purpose)
	}
	if got.Subject != hostile.Subject {
		t.Fatalf("the payload was reinterpreted: subject is %s", got.Subject)
	}

	// And it is still refused for the purpose it tried to smuggle.
	_, err = s.VerifyToken(token, PurposePasswordReset)
	if err == nil {
		t.Fatal("a verification token was accepted as a password-reset token")
	}
}

// A token minted for one purpose must not work for another, or a verification
// link — which is emailed on registration, to an address that may not be the
// account holder's — becomes a password reset.
func TestVerifyToken_RejectsTheWrongPurpose(t *testing.T) {
	s := testService(t)

	token, _, err := s.IssueToken(uuid.New(), PurposeEmailVerification, time.Hour)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	_, err = s.VerifyToken(token, PurposePasswordReset)
	if err == nil {
		t.Fatal("want a rejection for the wrong purpose, got nil")
	}
	assertStatus(t, err, http.StatusUnauthorized)
}

func TestVerifyToken_RejectsATamperedPayload(t *testing.T) {
	s := testService(t)

	token, _, err := s.IssueToken(uuid.New(), PurposePasswordReset, time.Hour)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	// Re-encode the payload with a different subject, keeping the original
	// signature — the forgery a signature exists to stop.
	encodedPayload, signature, _ := strings.Cut(token, tokenSeparator)
	raw, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}

	var payload TokenPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshalling: %v", err)
	}
	payload.Subject = uuid.New()

	forged, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	tampered := base64.RawURLEncoding.EncodeToString(forged) + tokenSeparator + signature

	if _, err := s.VerifyToken(tampered, PurposePasswordReset); err == nil {
		t.Fatal("a token with a substituted subject was accepted")
	}
}

func TestVerifyToken_RejectsAnotherKeysSignature(t *testing.T) {
	issuer := testService(t)
	other := &Service{bcryptCost: 4, tokenSecret: []byte("a completely different signing key")}

	token, _, err := other.IssueToken(uuid.New(), PurposePasswordReset, time.Hour)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	if _, err := issuer.VerifyToken(token, PurposePasswordReset); err == nil {
		t.Fatal("a token signed with a different key was accepted")
	}
}

func TestVerifyToken_RejectsAnExpiredToken(t *testing.T) {
	s := testService(t)

	token, _, err := s.IssueToken(uuid.New(), PurposePasswordReset, -time.Second)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	_, err = s.VerifyToken(token, PurposePasswordReset)
	if err == nil {
		t.Fatal("an expired token was accepted")
	}
	// Expiry is the one failure that is safe to distinguish: it tells the
	// holder of a genuine, signed token to ask for another one.
	assertStatus(t, err, http.StatusGone)
}

func TestVerifyToken_RejectsMalformedInput(t *testing.T) {
	s := testService(t)

	cases := map[string]string{
		"empty":             "",
		"no separator":      "notatoken",
		"empty payload":     "." + "c2ln",
		"bad base64":        "!!!!." + "c2ln",
		"bad signature b64": base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + ".!!!!",
		"not json":          base64.RawURLEncoding.EncodeToString([]byte(`not json`)) + ".c2ln",
	}

	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := s.VerifyToken(token, PurposePasswordReset); err == nil {
				t.Fatal("malformed input was accepted")
			}
		})
	}
}

// A signed payload naming the nil UUID must not be usable: it would address no
// account, and any code that looked it up would be querying on a zero value.
func TestVerifyToken_RejectsTheNilSubject(t *testing.T) {
	s := testService(t)

	token, _, err := s.IssueToken(uuid.Nil, PurposePasswordReset, time.Hour)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	if _, err := s.VerifyToken(token, PurposePasswordReset); err == nil {
		t.Fatal("a token for the nil subject was accepted")
	}
}

func TestFingerprint_IsStableAndDistinct(t *testing.T) {
	first := Fingerprint("token-a")
	again := Fingerprint("token-a")
	other := Fingerprint("token-b")

	// Stability is what lets a token presented later be matched against a
	// fingerprint stored when it was issued.
	if first != again {
		t.Fatal("the fingerprint of one token is not stable")
	}
	if first == other {
		t.Fatal("two tokens share a fingerprint")
	}
	// The stored value must not be the token itself, or a leaked table hands
	// out working links.
	if strings.Contains(first, "token-a") {
		t.Fatal("the fingerprint contains the token")
	}
}

// The token key must not be the JWT key, so that recovering one does not yield
// the other.
func TestDeriveTokenSecret_DiffersFromItsInput(t *testing.T) {
	secret := "the-jwt-signing-key-used-for-access-tokens"
	derived := deriveTokenSecret(secret)

	if string(derived) == secret {
		t.Fatal("the derived key is the input")
	}
	if len(derived) != tokenSecretLength {
		t.Fatalf("want %d bytes, got %d", tokenSecretLength, len(derived))
	}

	// Derivation must be deterministic, or a restart invalidates every
	// outstanding link.
	if string(deriveTokenSecret(secret)) != string(derived) {
		t.Fatal("derivation is not deterministic")
	}
	if string(deriveTokenSecret(secret+"x")) == string(derived) {
		t.Fatal("two different secrets derived the same key")
	}
}

// NewService must derive when TOKEN_SECRET is unset and use it when it is set;
// getting this backwards would silently sign with an empty key.
func TestNewService_UsesConfiguredTokenSecretWhenPresent(t *testing.T) {
	explicit := "an-explicitly-configured-token-signing-secret"

	derivedSvc := NewService(&config.Config{Auth: config.Auth{
		JWTSecret:  "the-jwt-secret",
		BcryptCost: 4,
	}})
	explicitSvc := NewService(&config.Config{Auth: config.Auth{
		JWTSecret:   "the-jwt-secret",
		TokenSecret: explicit,
		BcryptCost:  4,
	}})

	if string(explicitSvc.tokenSecret) != explicit {
		t.Fatal("an explicit TOKEN_SECRET was not used verbatim")
	}
	if string(derivedSvc.tokenSecret) == explicit {
		t.Fatal("the derived key matched the explicit one")
	}
	if string(derivedSvc.tokenSecret) == "the-jwt-secret" {
		t.Fatal("the derived key is JWT_SECRET itself")
	}
}

// assertStatus checks the classification rather than the message.
func assertStatus(t *testing.T, err error, want int) {
	t.Helper()
	var appErr *apperrors.AppError
	if !apperrors.As(err, &appErr) {
		t.Fatalf("want an *AppError, got %T: %v", err, err)
	}
	if appErr.ToHTTPStatus() != want {
		t.Fatalf("want status %d, got %d (%s)", want, appErr.ToHTTPStatus(), appErr.Type)
	}
}

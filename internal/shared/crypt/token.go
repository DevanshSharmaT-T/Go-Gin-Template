// File: internal/shared/crypt/token.go

package crypt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"hash"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// Purpose says what a token may be used for. It is signed into the payload and
// checked on verification, so a token minted to confirm an email address cannot
// be replayed against the password-reset endpoint.
type Purpose string

const (
	// PurposeEmailVerification confirms that an address belongs to whoever
	// registered it.
	PurposeEmailVerification Purpose = "email_verification"
	// PurposePasswordReset authorises setting a new password without knowing
	// the old one.
	PurposePasswordReset Purpose = "password_reset"
)

// tokenSeparator joins the payload and its signature.
const tokenSeparator = "."

// TokenPayload is the signed content of a verification token.
//
// It is deliberately small. Everything in here is visible to whoever holds the
// token — base64 is encoding, not encryption — so it carries an identifier and
// a validity window and nothing else. No email address, no name, no role.
type TokenPayload struct {
	Subject   uuid.UUID `json:"sub"`
	Purpose   Purpose   `json:"purpose"`
	Nonce     string    `json:"nonce"`
	IssuedAt  int64     `json:"iat"`
	ExpiresAt int64     `json:"exp"`
}

// Expired reports whether the payload's validity window has closed.
func (p *TokenPayload) Expired(now time.Time) bool {
	return now.Unix() >= p.ExpiresAt
}

// IssueToken mints a signed, single-use token for subject.
//
// The wire format is:
//
//	base64url(json payload) "." base64url(hmac-sha256 of the json payload)
//
// **The payload is JSON, and the signature covers the exact JSON bytes.** That
// is the whole security argument for this design, and it is worth stating
// plainly because the tempting alternative is worse. An ad-hoc payload like
//
//	user=<id>|purpose=reset|exp=<ts>
//
// is unambiguous only while no field can contain the delimiter. Let a value
// carry a "|" and an attacker appends fields of their choosing; the server
// signs the whole thing, so the signature verifies, and the *parse* is what
// they control. JSON has one unambiguous encoding of any string, and the
// signature is taken over the serialised bytes rather than over a
// reconstruction of them, so there is no second interpretation to disagree
// with the first.
//
// The returned token is single-use only if the caller records it — see
// [Fingerprint]. Nothing in the token itself can enforce that, because a
// stateless token has no way to know it has been seen before.
func (s *Service) IssueToken(subject uuid.UUID, purpose Purpose, ttl time.Duration) (string, *TokenPayload, error) {
	var nonce string
	var err error
	nonce, err = RandomString(nonceBytes)
	if err != nil {
		return "", nil, err
	}

	var now time.Time = time.Now().UTC()
	var payload *TokenPayload = &TokenPayload{
		Subject:   subject,
		Purpose:   purpose,
		Nonce:     nonce,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
	}

	var encoded []byte
	encoded, err = json.Marshal(payload)
	if err != nil {
		return "", nil, errors.NewInternalError("could not serialise the token payload", err)
	}

	var token string = base64.RawURLEncoding.EncodeToString(encoded) +
		tokenSeparator +
		base64.RawURLEncoding.EncodeToString(s.sign(encoded))

	return token, payload, nil
}

// VerifyToken checks a token's signature, purpose and expiry, and returns its
// payload.
//
// The order is load-bearing: the signature is verified over the raw decoded
// bytes **before** they are unmarshalled. Parsing first would mean running a
// JSON decoder over attacker-supplied input and then deciding whether to trust
// it, which is the same class of mistake as validating a signature over a
// re-serialisation of the parsed value.
//
// Every failure returns the same classification and an unhelpful message. A
// caller cannot tell a bad signature from an expired token from a token for the
// wrong purpose, because the difference is only useful to someone probing.
func (s *Service) VerifyToken(token string, purpose Purpose) (*TokenPayload, error) {
	var encodedPayload string
	var encodedSignature string
	var found bool
	encodedPayload, encodedSignature, found = strings.Cut(token, tokenSeparator)
	if !found {
		return nil, errTokenInvalid(nil)
	}

	var payloadBytes []byte
	var err error
	payloadBytes, err = base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return nil, errTokenInvalid(err)
	}

	var signature []byte
	signature, err = base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil {
		return nil, errTokenInvalid(err)
	}

	// Constant-time. A byte-by-byte == would leak how much of a forged
	// signature was right, which is enough to build the rest one byte at a
	// time.
	if !hmac.Equal(signature, s.sign(payloadBytes)) {
		return nil, errTokenInvalid(nil)
	}

	var payload *TokenPayload = &TokenPayload{}
	err = json.Unmarshal(payloadBytes, payload)
	if err != nil {
		return nil, errTokenInvalid(err)
	}

	if payload.Purpose != purpose {
		return nil, errTokenInvalid(nil)
	}
	if payload.Subject == uuid.Nil {
		return nil, errTokenInvalid(nil)
	}
	if payload.Expired(time.Now().UTC()) {
		return nil, errors.NewGoneError("this link has expired; request a new one", nil)
	}

	return payload, nil
}

// Fingerprint returns the SHA-256 of a token, hex-encoded.
//
// This is what gets stored to make a token single-use. Storing the fingerprint
// rather than the token means a leaked database backup does not hand over
// working reset links: the stored value confirms a token presented later, and
// cannot be turned back into one.
//
// SHA-256 rather than bcrypt is right here, unlike for a password: the token is
// 128 bits of uniform randomness, so there is no dictionary to run and nothing
// for a slow hash to buy.
func Fingerprint(token string) string {
	var sum [sha256.Size]byte = sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// sign returns the HMAC-SHA256 of message under the service's token key.
func (s *Service) sign(message []byte) []byte {
	var mac hash.Hash = hmac.New(sha256.New, s.tokenSecret)
	// hash.Hash.Write is documented never to return an error.
	_, _ = mac.Write(message)
	return mac.Sum(nil)
}

// errTokenInvalid is the single failure every malformed, forged or misdirected
// token produces.
func errTokenInvalid(cause error) *errors.AppError {
	return errors.NewTokenInvalidError("this link is not valid", cause)
}

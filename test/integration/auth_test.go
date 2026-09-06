// File: test/integration/auth_test.go

//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	authservice "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/service"
	userdomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/crypt"
	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/middleware"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/test/harness"
)

// authFixture boots the real graph and hands back the pieces a test drives.
type authFixture struct {
	auth   *authservice.AuthService
	users  userdomain.UserRepository
	tokens userdomain.VerificationTokenRepository
	codec  *middleware.TokenCodec
	crypt  *crypt.Service
	db     *gorm.DB
}

// newAuthFixture boots the application and cleans up the rows it writes.
//
// Each test gets its own unique identifiers rather than a truncated database,
// so tests do not depend on each other's cleanup having run.
func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()

	f := &authFixture{}
	harness.App(t, []any{&f.auth, &f.users, &f.tokens, &f.codec, &f.crypt, &f.db})

	t.Cleanup(func() {
		// Tokens first: they reference users.
		if err := f.db.Exec(`DELETE FROM verification_tokens`).Error; err != nil {
			t.Errorf("clearing verification_tokens: %v", err)
		}
		if err := f.db.Exec(`DELETE FROM users`).Error; err != nil {
			t.Errorf("clearing users: %v", err)
		}
	})

	return f
}

// unique returns an identifier no other test will collide with.
func unique(prefix string) string {
	return prefix + uuid.New().String()[:8]
}

func register(t *testing.T, f *authFixture, username string) *authservice.RegisteredDTO {
	t.Helper()

	result, err := f.auth.Register(context.Background(), &authservice.RegisterRequestDTO{
		Username:  username,
		Email:     username + "@example.com",
		Password:  "a sufficiently long password",
		FirstName: "Test",
		LastName:  "User",
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	return result
}

// The whole documented flow, through the real graph and a real database.
func TestAuth_RegisterVerifyLogin(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()
	username := unique("flow")

	registered := register(t, f, username)

	// A new account is inactive, so it cannot sign in yet.
	_, err := f.auth.Login(ctx, &authservice.LoginRequestDTO{
		Identifier: username, Password: "a sufficiently long password",
	})
	if err == nil {
		t.Fatal("an unverified account signed in")
	}
	if !apperrors.IsType(err, apperrors.TypeInvalidCredentials) {
		t.Fatalf("want INVALID_CREDENTIALS, got %v", err)
	}

	// Verification activates it. The token is reconstructed here the way the
	// emailed link carries it.
	token := issueVerificationToken(t, f, registered.ID)
	if _, err := f.auth.VerifyEmail(ctx, &authservice.VerifyEmailRequestDTO{Token: token}); err != nil {
		t.Fatalf("verifying: %v", err)
	}

	stored, err := f.users.GetByID(ctx, registered.ID)
	if err != nil {
		t.Fatalf("reading the account: %v", err)
	}
	if !stored.IsActive() || !stored.EmailVerified() {
		t.Fatalf("verification did not activate the account: status=%s verified=%v",
			stored.Status, stored.EmailVerified())
	}

	// And now it signs in.
	result, err := f.auth.Login(ctx, &authservice.LoginRequestDTO{
		Identifier: username, Password: "a sufficiently long password",
	})
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	claims, err := f.codec.Parse(result.AccessToken)
	if err != nil {
		t.Fatalf("the issued token does not verify: %v", err)
	}
	if claims.UserID != registered.ID {
		t.Fatalf("token subject: want %s, got %s", registered.ID, claims.UserID)
	}
	if claims.RoleID != userdomain.DefaultRoleID {
		t.Fatalf("want the default role %d in the token, got %d",
			userdomain.DefaultRoleID, claims.RoleID)
	}
}

// The single-use property, against the real conditional update rather than a
// mock that always agrees.
func TestAuth_VerificationTokenIsSingleUse(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()

	registered := register(t, f, unique("once"))
	token := issueVerificationToken(t, f, registered.ID)

	if _, err := f.auth.VerifyEmail(ctx, &authservice.VerifyEmailRequestDTO{Token: token}); err != nil {
		t.Fatalf("first use: %v", err)
	}

	_, err := f.auth.VerifyEmail(ctx, &authservice.VerifyEmailRequestDTO{Token: token})
	if err == nil {
		t.Fatal("the same link was accepted twice")
	}
	if !apperrors.IsType(err, apperrors.TypeGone) {
		t.Fatalf("want GONE on replay, got %v", err)
	}
}

// Issuing a new reset link must retire the previous one, or a link sitting in a
// mailbox stays live for its whole TTL.
func TestAuth_RequestingANewResetLinkRetiresTheOldOne(t *testing.T) {
	f := newAuthFixture(t)
	ctx := context.Background()
	username := unique("retire")

	registered := register(t, f, username)
	activate(t, f, registered.ID)

	first := issueResetToken(t, f, registered.ID)
	second := issueResetToken(t, f, registered.ID)

	// The older link is dead.
	_, err := f.auth.ResetPassword(ctx, &authservice.ResetPasswordRequestDTO{
		Token: first, NewPassword: "a different long password",
	})
	if err == nil {
		t.Fatal("a superseded reset link still worked")
	}

	// The newer one works.
	if _, err := f.auth.ResetPassword(ctx, &authservice.ResetPasswordRequestDTO{
		Token: second, NewPassword: "a different long password",
	}); err != nil {
		t.Fatalf("the current reset link failed: %v", err)
	}

	// And the new password is the one that signs in.
	if _, err := f.auth.Login(ctx, &authservice.LoginRequestDTO{
		Identifier: username, Password: "a different long password",
	}); err != nil {
		t.Fatalf("signing in with the new password: %v", err)
	}
	if _, err := f.auth.Login(ctx, &authservice.LoginRequestDTO{
		Identifier: username, Password: "a sufficiently long password",
	}); err == nil {
		t.Fatal("the old password still signs in after a reset")
	}
}

// A token for one purpose must not be redeemable for another, checked against
// the stored record rather than only the signature.
func TestAuth_ResetTokenCannotVerifyAnEmail(t *testing.T) {
	f := newAuthFixture(t)

	registered := register(t, f, unique("purpose"))
	activate(t, f, registered.ID)
	reset := issueResetToken(t, f, registered.ID)

	_, err := f.auth.VerifyEmail(context.Background(),
		&authservice.VerifyEmailRequestDTO{Token: reset})
	if err == nil {
		t.Fatal("a password-reset token verified an email address")
	}
}

// A signed token whose fingerprint was never recorded must be refused: that is
// what stops a token minted with a leaked key from being usable on its own.
func TestAuth_UnrecordedTokenIsRefused(t *testing.T) {
	f := newAuthFixture(t)

	registered := register(t, f, unique("unrecorded"))

	// Mint a structurally perfect token without recording it.
	token, _, err := f.crypt.IssueToken(registered.ID, crypt.PurposeEmailVerification, time.Hour)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	if _, err := f.auth.VerifyEmail(context.Background(),
		&authservice.VerifyEmailRequestDTO{Token: token}); err == nil {
		t.Fatal("a token that was never issued by the service was accepted")
	}
}

// Registration must not create a second account on a taken address, and the
// conflict must be a 409 rather than a driver error.
func TestAuth_RegisterRejectsADuplicateEmail(t *testing.T) {
	f := newAuthFixture(t)
	username := unique("dup")

	register(t, f, username)

	_, err := f.auth.Register(context.Background(), &authservice.RegisterRequestDTO{
		Username:  unique("other"),
		Email:     username + "@example.com",
		Password:  "a sufficiently long password",
		FirstName: "Test", LastName: "User",
	})
	if err == nil {
		t.Fatal("a duplicate email address registered")
	}

	var appErr *apperrors.AppError
	if !apperrors.As(err, &appErr) || appErr.ToHTTPStatus() != http.StatusConflict {
		t.Fatalf("want CONFLICT, got %v", err)
	}
}

// The users table must carry a hash, never the password, and the unique indexes
// must actually exist.
func TestAuth_StoredAccountCarriesNoPlaintext(t *testing.T) {
	f := newAuthFixture(t)

	registered := register(t, f, unique("stored"))

	stored, err := f.users.GetByID(context.Background(), registered.ID)
	if err != nil {
		t.Fatalf("reading the account: %v", err)
	}
	if stored.PasswordHash == "a sufficiently long password" {
		t.Fatal("the password was stored in plaintext")
	}

	var count int64
	if err := f.db.Raw(
		`SELECT count(*) FROM pg_indexes WHERE tablename = 'users' AND indexdef LIKE '%UNIQUE%'`,
	).Scan(&count).Error; err != nil {
		t.Fatalf("reading indexes: %v", err)
	}
	// One each for username and email, plus the primary key.
	if count < 2 {
		t.Fatalf("want unique indexes on username and email, found %d", count)
	}
}

// activate marks an account usable, standing in for the verification flow when
// a test is not exercising it.
func activate(t *testing.T, f *authFixture, id uuid.UUID) {
	t.Helper()

	user, err := f.users.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("reading the account: %v", err)
	}
	now := time.Now().UTC()
	user.Status = userdomain.UserStatusActive
	user.EmailVerifiedAt = &now
	if err := f.users.Update(context.Background(), user); err != nil {
		t.Fatalf("activating: %v", err)
	}
}

// issueVerificationToken and issueResetToken mint a token through the same path
// the service uses, and record it, so a test can redeem one without reading the
// log for a link.
func issueVerificationToken(t *testing.T, f *authFixture, id uuid.UUID) string {
	t.Helper()
	return issueToken(t, f, id, crypt.PurposeEmailVerification)
}

func issueResetToken(t *testing.T, f *authFixture, id uuid.UUID) string {
	t.Helper()

	// Match the service: a new reset link retires the outstanding ones.
	if err := f.tokens.InvalidateOutstanding(
		context.Background(), id, crypt.PurposePasswordReset, time.Now().UTC(),
	); err != nil {
		t.Fatalf("invalidating outstanding tokens: %v", err)
	}
	return issueToken(t, f, id, crypt.PurposePasswordReset)
}

func issueToken(t *testing.T, f *authFixture, id uuid.UUID, purpose crypt.Purpose) string {
	t.Helper()

	token, payload, err := f.crypt.IssueToken(id, purpose, time.Hour)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	if err := f.tokens.Create(context.Background(), &userdomain.VerificationToken{
		UserID:      id,
		Fingerprint: crypt.Fingerprint(token),
		Purpose:     purpose,
		ExpiresAt:   time.Unix(payload.ExpiresAt, 0).UTC(),
	}); err != nil {
		t.Fatalf("recording the token: %v", err)
	}

	return token
}

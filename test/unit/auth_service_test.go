// File: test/unit/auth_service_test.go

// Package unit holds service-level tests that use generated mocks and touch no
// I/O. They run on every `go test ./...`, with no database.
package unit

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	mockusers "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/mocks/users"
	authdomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/domain"
	authservice "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/service"
	userdomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/crypt"
	apperrors "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/mail"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/middleware"
)

// testConfig is the smallest configuration the auth service needs.
func testConfig() *config.Config {
	return &config.Config{
		Auth: config.Auth{
			JWTSecret:             "a-unit-test-signing-key-of-sufficient-length",
			JWTIssuer:             "unit-test",
			JWTTTL:                time.Hour,
			BcryptCost:            4,
			VerificationTokenTTL:  time.Hour,
			PasswordResetTokenTTL: time.Hour,
		},
		Frontend: config.Frontend{URL: "http://localhost:3000"},
	}
}

// recordingMailer captures what would have been sent.
type recordingMailer struct {
	sent []mail.Message
}

func (m *recordingMailer) Send(_ context.Context, message mail.Message) error {
	m.sent = append(m.sent, message)
	return nil
}

type authFixture struct {
	service *authservice.AuthService
	users   *mockusers.MockUserRepository
	tokens  *mockusers.MockVerificationTokenRepository
	mailer  *recordingMailer
	crypt   *crypt.Service
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	cfg := testConfig()

	users := mockusers.NewMockUserRepository(ctrl)
	tokens := mockusers.NewMockVerificationTokenRepository(ctrl)
	mailer := &recordingMailer{}
	cryptSvc := crypt.NewService(cfg)

	return &authFixture{
		service: authservice.NewAuthService(
			cfg, users, tokens, cryptSvc,
			middleware.NewTokenCodec(cfg),
			&stubRoleResolver{},
			mailer,
		),
		users:  users,
		tokens: tokens,
		mailer: mailer,
		crypt:  cryptSvc,
	}
}

// The regression test for privilege escalation by JSON field. The request type
// has no role field to bind, so the assertion is on what actually reaches the
// repository: the server-side default, every time.
func TestAuthService_Register_AlwaysAssignsTheDefaultRole(t *testing.T) {
	f := newAuthFixture(t)

	f.users.EXPECT().FindByEmail(gomock.Any(), "new@example.com").Return(nil, nil)
	f.users.EXPECT().FindByUsername(gomock.Any(), "newcomer").Return(nil, nil)

	var created *userdomain.User
	f.users.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, user *userdomain.User) error {
			created = user
			user.ID = uuid.New()
			return nil
		})
	f.tokens.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

	_, err := f.service.Register(context.Background(), &authservice.RegisterRequestDTO{
		Username:  "Newcomer",
		Email:     "New@Example.com",
		Password:  "a sufficiently long password",
		FirstName: "New",
		LastName:  "Comer",
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	if created.RoleID != userdomain.DefaultRoleID {
		t.Fatalf("want the default role %d, got %d", userdomain.DefaultRoleID, created.RoleID)
	}

	// A new account is unverified and unusable until the address is confirmed.
	if created.Status != userdomain.UserStatusInactive {
		t.Fatalf("want a new account inactive, got %q", created.Status)
	}
	if created.EmailVerifiedAt != nil {
		t.Fatal("a new account was marked as having a verified address")
	}

	// Normalisation happens before the unique index sees the value.
	if created.Email != "new@example.com" || created.Username != "newcomer" {
		t.Fatalf("identifiers were not normalised: %q / %q", created.Email, created.Username)
	}

	// The password is hashed, and the plaintext is nowhere on the entity.
	if created.PasswordHash == "a sufficiently long password" || created.PasswordHash == "" {
		t.Fatal("the password was not hashed")
	}
	if err := f.crypt.ComparePassword(created.PasswordHash, "a sufficiently long password"); err != nil {
		t.Fatalf("the stored hash does not verify the password: %v", err)
	}
}

func TestAuthService_Register_SendsAVerificationLink(t *testing.T) {
	f := newAuthFixture(t)

	f.users.EXPECT().FindByEmail(gomock.Any(), gomock.Any()).Return(nil, nil)
	f.users.EXPECT().FindByUsername(gomock.Any(), gomock.Any()).Return(nil, nil)
	f.users.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, user *userdomain.User) error {
			user.ID = uuid.New()
			return nil
		})

	// The token's fingerprint is recorded, which is what makes it single-use.
	var recorded *userdomain.VerificationToken
	f.tokens.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, token *userdomain.VerificationToken) error {
			recorded = token
			return nil
		})

	_, err := f.service.Register(context.Background(), &authservice.RegisterRequestDTO{
		Username: "sender", Email: "sender@example.com",
		Password: "a sufficiently long password", FirstName: "S", LastName: "R",
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	if len(f.mailer.sent) != 1 {
		t.Fatalf("want one email, got %d", len(f.mailer.sent))
	}
	if f.mailer.sent[0].To != "sender@example.com" {
		t.Fatalf("addressed to %q", f.mailer.sent[0].To)
	}
	if recorded.Purpose != crypt.PurposeEmailVerification {
		t.Fatalf("want an email-verification token, got %q", recorded.Purpose)
	}
	// Only the fingerprint is stored, never the token itself.
	if recorded.Fingerprint == "" {
		t.Fatal("no fingerprint was recorded, so the token would not be single-use")
	}
}

// The regression test for BE-3, and for the enumeration oracle beside it.
//
// An inactive or suspended account must fail *before* the password is checked,
// and with exactly the message an unknown account produces — a distinct
// "account disabled" reply confirms the address is registered.
func TestAuthService_Login_RejectsNonActiveAccountsIndistinguishably(t *testing.T) {
	reference := loginFailureFor(t, nil) // unknown identifier

	for _, status := range []userdomain.UserStatus{
		userdomain.UserStatusInactive,
		userdomain.UserStatusSuspended,
	} {
		t.Run(string(status), func(t *testing.T) {
			f := newAuthFixture(t)
			hash, err := f.crypt.HashPassword("the correct password")
			if err != nil {
				t.Fatalf("hashing: %v", err)
			}

			f.users.EXPECT().FindByIdentifier(gomock.Any(), "someone").
				Return(&userdomain.User{
					ID: uuid.New(), Username: "someone", Email: "someone@example.com",
					PasswordHash: hash, Status: status,
				}, nil)
			// No Update call is expected: a rejected login must not touch the row.

			_, err = f.service.Login(context.Background(), &authservice.LoginRequestDTO{
				Identifier: "someone",
				Password:   "the correct password", // deliberately correct
			})
			if err == nil {
				t.Fatalf("a %s account signed in with a correct password", status)
			}

			assertSameFailure(t, reference, err)
		})
	}
}

func TestAuthService_Login_WrongPasswordIsIndistinguishableFromUnknownUser(t *testing.T) {
	reference := loginFailureFor(t, nil)

	f := newAuthFixture(t)
	hash, err := f.crypt.HashPassword("the correct password")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	f.users.EXPECT().FindByIdentifier(gomock.Any(), "someone").
		Return(&userdomain.User{
			ID: uuid.New(), PasswordHash: hash, Status: userdomain.UserStatusActive,
		}, nil)

	_, err = f.service.Login(context.Background(), &authservice.LoginRequestDTO{
		Identifier: "someone",
		Password:   "the wrong password",
	})
	if err == nil {
		t.Fatal("a wrong password signed in")
	}
	assertSameFailure(t, reference, err)
}

func TestAuthService_Login_ActiveAccountReceivesAToken(t *testing.T) {
	f := newAuthFixture(t)

	hash, err := f.crypt.HashPassword("the correct password")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	user := &userdomain.User{
		ID: uuid.New(), RoleID: userdomain.DefaultRoleID,
		PasswordHash: hash, Status: userdomain.UserStatusActive,
	}

	f.users.EXPECT().FindByIdentifier(gomock.Any(), "someone").Return(user, nil)
	f.users.EXPECT().Update(gomock.Any(), user).Return(nil)

	result, err := f.service.Login(context.Background(), &authservice.LoginRequestDTO{
		Identifier: "someone", Password: "the correct password",
	})
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	if result.AccessToken == "" || result.TokenType != "Bearer" {
		t.Fatalf("unexpected token response: %+v", result)
	}
	if user.LastLoginAt == nil {
		t.Fatal("the login timestamp was not recorded")
	}

	// The token must carry what the middleware will read back out.
	claims, err := middleware.NewTokenCodec(testConfig()).Parse(result.AccessToken)
	if err != nil {
		t.Fatalf("the issued token does not verify: %v", err)
	}
	if claims.UserID != user.ID {
		t.Fatalf("token subject: want %s, got %s", user.ID, claims.UserID)
	}
}

// The forgot-password endpoint must answer identically whether or not the
// account exists, or it is a membership oracle for any list of addresses.
func TestAuthService_RequestPasswordReset_SaysTheSameThingForUnknownAccounts(t *testing.T) {
	known := newAuthFixture(t)
	user := &userdomain.User{ID: uuid.New(), Email: "known@example.com", Status: userdomain.UserStatusActive}
	known.users.EXPECT().FindByIdentifier(gomock.Any(), "known@example.com").Return(user, nil)
	known.tokens.EXPECT().InvalidateOutstanding(gomock.Any(), user.ID, crypt.PurposePasswordReset, gomock.Any()).Return(nil)
	known.tokens.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

	knownResult, err := known.service.RequestPasswordReset(context.Background(),
		&authservice.ForgotPasswordRequestDTO{Identifier: "known@example.com"})
	if err != nil {
		t.Fatalf("requesting: %v", err)
	}

	unknown := newAuthFixture(t)
	unknown.users.EXPECT().FindByIdentifier(gomock.Any(), "nobody@example.com").Return(nil, nil)

	unknownResult, err := unknown.service.RequestPasswordReset(context.Background(),
		&authservice.ForgotPasswordRequestDTO{Identifier: "nobody@example.com"})
	if err != nil {
		t.Fatalf("requesting: %v", err)
	}

	if knownResult.Message != unknownResult.Message {
		t.Fatalf("the responses differ, which enumerates accounts:\n known:   %q\n unknown: %q",
			knownResult.Message, unknownResult.Message)
	}

	// The known account got a link; the unknown one did not.
	if len(known.mailer.sent) != 1 {
		t.Fatalf("want one email for the known account, got %d", len(known.mailer.sent))
	}
	if len(unknown.mailer.sent) != 0 {
		t.Fatalf("an email was sent for an account that does not exist")
	}
}

// The regression test for a password hash in the reset URL. The link carries an
// opaque token and nothing else.
func TestAuthService_RequestPasswordReset_LinkCarriesNoHash(t *testing.T) {
	f := newAuthFixture(t)

	hash, err := f.crypt.HashPassword("the current password")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	user := &userdomain.User{
		ID: uuid.New(), Email: "user@example.com",
		PasswordHash: hash, Status: userdomain.UserStatusActive,
	}

	f.users.EXPECT().FindByIdentifier(gomock.Any(), gomock.Any()).Return(user, nil)
	f.tokens.EXPECT().InvalidateOutstanding(gomock.Any(), user.ID, crypt.PurposePasswordReset, gomock.Any()).Return(nil)
	f.tokens.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

	_, err = f.service.RequestPasswordReset(context.Background(),
		&authservice.ForgotPasswordRequestDTO{Identifier: "user@example.com"})
	if err != nil {
		t.Fatalf("requesting: %v", err)
	}

	body := f.mailer.sent[0].Text
	if containsAny(body, hash, "$2a$", "$2b$", "password_hash") {
		t.Fatalf("the reset email carries the password hash:\n%s", body)
	}
}

// loginFailureFor produces the failure an unknown identifier gives, which is
// the reference every other login failure must match.
func loginFailureFor(t *testing.T, user *userdomain.User) *apperrors.AppError {
	t.Helper()

	f := newAuthFixture(t)
	f.users.EXPECT().FindByIdentifier(gomock.Any(), gomock.Any()).Return(user, nil)

	_, err := f.service.Login(context.Background(), &authservice.LoginRequestDTO{
		Identifier: "whoever", Password: "whatever",
	})
	if err == nil {
		t.Fatal("expected a failure")
	}

	var appErr *apperrors.AppError
	if !apperrors.As(err, &appErr) {
		t.Fatalf("want an *AppError, got %T", err)
	}
	return appErr
}

// assertSameFailure checks that two failures are indistinguishable to a client:
// same classification, same status, same rendered body.
func assertSameFailure(t *testing.T, reference *apperrors.AppError, got error) {
	t.Helper()

	var appErr *apperrors.AppError
	if !apperrors.As(got, &appErr) {
		t.Fatalf("want an *AppError, got %T: %v", got, got)
	}

	if appErr.Type != reference.Type {
		t.Fatalf("classification differs: want %s, got %s", reference.Type, appErr.Type)
	}
	if appErr.ToHTTPStatus() != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", appErr.ToHTTPStatus())
	}
	if appErr.Response().Error != reference.Response().Error {
		t.Fatalf("the message differs, which enumerates accounts:\n want: %q\n got:  %q",
			reference.Response().Error, appErr.Response().Error)
	}
}

// containsAny reports whether any needle appears in haystack.
func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

// stubRoleResolver stands in for the roles module, which arrives in the RBAC
// phase. It reports the least senior level and no permissions.
type stubRoleResolver struct{}

func (stubRoleResolver) Resolve(_ context.Context, roleID int64) (*authdomain.RoleClaims, error) {
	return &authdomain.RoleClaims{
		RoleID:         roleID,
		HierarchyLevel: authdomain.UnprivilegedHierarchyLevel,
		Permissions:    map[string]bool{},
	}, nil
}

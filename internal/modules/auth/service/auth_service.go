// File: internal/modules/auth/service/auth_service.go

package service

import (
	"context"
	"net/url"
	"time"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/config"
	authdomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/auth/domain"
	userdomain "github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/crypt"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/logger"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/mail"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/middleware"
)

// genericAuthFailure is the single message every login failure returns.
//
// Unknown username, wrong password, unverified address, suspended account — all
// of them produce this. Distinguishing them tells an attacker which half of the
// pair was right, which turns the endpoint into a user-enumeration oracle and
// halves the work of credential stuffing. The specific reason goes to the log.
const genericAuthFailure = "invalid credentials"

// genericResetAcknowledgement is returned by the forgot-password endpoint
// whether or not the account exists, for the same reason.
const genericResetAcknowledgement = "if that account exists, a password reset link has been sent"

// AuthService owns the credential lifecycle: registering, signing in, proving
// an address, and recovering a forgotten password.
//
// It has no infra/ of its own. It borrows the users module's repositories,
// because the entity it operates on belongs to that module — auth changes the
// *rules* about accounts, not their shape.
type AuthService struct {
	cfg    *config.Config
	users  userdomain.UserRepository
	tokens userdomain.VerificationTokenRepository
	crypt  *crypt.Service
	codec  *middleware.TokenCodec
	roles  authdomain.RoleResolver
	mailer mail.Mailer
}

// NewAuthService builds the service.
func NewAuthService(
	cfg *config.Config,
	users userdomain.UserRepository,
	tokens userdomain.VerificationTokenRepository,
	cryptSvc *crypt.Service,
	codec *middleware.TokenCodec,
	roles authdomain.RoleResolver,
	mailer mail.Mailer,
) *AuthService {
	return &AuthService{
		cfg:    cfg,
		users:  users,
		tokens: tokens,
		crypt:  cryptSvc,
		codec:  codec,
		roles:  roles,
		mailer: mailer,
	}
}

// Register creates an unverified account and emails a verification link.
//
// The new account is inactive and cannot sign in until the address is
// confirmed. That is what makes registering an address you do not control
// pointless, and it is what gives password reset something to rely on.
//
// The role is assigned here, from a constant. See RegisterRequestDTO.
func (s *AuthService) Register(ctx context.Context, req *RegisterRequestDTO) (*RegisteredDTO, error) {
	var username string = userdomain.NormalizeUsername(req.Username)
	var email string = userdomain.NormalizeEmail(req.Email)

	var err error
	err = userdomain.ValidateUsername(username)
	if err != nil {
		return nil, err
	}
	err = userdomain.ValidateEmail(email)
	if err != nil {
		return nil, err
	}
	err = userdomain.ValidatePassword(req.Password)
	if err != nil {
		return nil, err
	}
	err = userdomain.ValidateName("first_name", req.FirstName)
	if err != nil {
		return nil, err
	}
	err = userdomain.ValidateName("last_name", req.LastName)
	if err != nil {
		return nil, err
	}

	// Checked explicitly so the response names the field. The unique indexes
	// are still the authority — two simultaneous registrations can both pass
	// this check — and the CONFLICT the adapter translates is the backstop.
	var existing *userdomain.User
	existing, err = s.users.FindByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.NewConflictError("that email address is already registered", nil).
			WithDetail("email", "is already registered")
	}

	existing, err = s.users.FindByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.NewConflictError("that username is already taken", nil).
			WithDetail("username", "is already taken")
	}

	var hashed string
	hashed, err = s.crypt.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	var user *userdomain.User = &userdomain.User{
		Username:     username,
		Email:        email,
		PasswordHash: hashed,
		FirstName:    req.FirstName,
		LastName:     req.LastName,
		RoleID:       userdomain.DefaultRoleID,
		Status:       userdomain.UserStatusInactive,
	}

	err = s.users.Create(ctx, user)
	if err != nil {
		return nil, err
	}

	// A failure to send must not undo a successful registration: the account
	// exists, and the address can be verified from a link requested later.
	err = s.sendVerificationLink(ctx, user)
	if err != nil {
		logger.FromContext(ctx).Error().Err(err).
			Str("user_id", user.ID.String()).
			Msg("could not send the verification email; the account was still created")
	}

	return &RegisteredDTO{
		ID:       user.ID,
		Username: user.Username,
		Email:    user.Email,
		Message:  "account created; check your email for a verification link",
	}, nil
}

// Login authenticates and issues an access token.
//
// The order of checks is the security-relevant part.
//
//  1. An unknown identifier still runs a bcrypt comparison, against a fixed
//     hash, before failing. Skipping it would make "no such user" measurably
//     faster than "wrong password" — a timing oracle that enumerates accounts.
//  2. The account state is checked *before* the password, and an inactive or
//     suspended account fails with the same message as a wrong password. A
//     disabled account can still hold a valid password, and answering "your
//     account is disabled" confirms the address is registered.
func (s *AuthService) Login(ctx context.Context, req *LoginRequestDTO) (*AuthResponseDTO, error) {
	var log = logger.FromContext(ctx)

	var user *userdomain.User
	var err error
	user, err = s.users.FindByIdentifier(ctx, req.Identifier)
	if err != nil {
		return nil, err
	}

	if user == nil {
		s.crypt.DummyCompare(req.Password)
		log.Info().Msg("login failed: no account for that identifier")
		return nil, errors.NewInvalidCredentialsError(genericAuthFailure, nil)
	}

	if !user.IsActive() {
		s.crypt.DummyCompare(req.Password)
		log.Info().
			Str("user_id", user.ID.String()).
			Str("status", user.Status.String()).
			Msg("login failed: account is not active")
		return nil, errors.NewInvalidCredentialsError(genericAuthFailure, nil)
	}

	err = s.crypt.ComparePassword(user.PasswordHash, req.Password)
	if err != nil {
		if errors.IsType(err, errors.TypeInvalidCredentials) {
			log.Info().Str("user_id", user.ID.String()).Msg("login failed: wrong password")
			return nil, errors.NewInvalidCredentialsError(genericAuthFailure, err)
		}
		return nil, err
	}

	// The password was correct, so it can be re-hashed at the current cost
	// without asking for it again. This is how raising BCRYPT_COST rolls out
	// without a migration. A failure here is not worth failing the login over.
	s.rehashIfNeeded(ctx, user, req.Password)

	var now time.Time = time.Now().UTC()
	user.LastLoginAt = &now
	err = s.users.Update(ctx, user)
	if err != nil {
		// The sign-in succeeded; only the bookkeeping failed.
		log.Error().Err(err).Str("user_id", user.ID.String()).
			Msg("could not record the login timestamp")
	}

	return s.issueToken(ctx, user)
}

// issueToken resolves the user's role and signs an access token.
func (s *AuthService) issueToken(ctx context.Context, user *userdomain.User) (*AuthResponseDTO, error) {
	var roleClaims *authdomain.RoleClaims
	var err error
	roleClaims, err = s.roles.Resolve(ctx, user.RoleID)
	if err != nil {
		return nil, err
	}

	var claims *middleware.Claims = &middleware.Claims{
		UserID:         user.ID,
		RoleID:         roleClaims.RoleID,
		HierarchyLevel: roleClaims.HierarchyLevel,
		Permissions:    roleClaims.Permissions,
		PermVersion:    roleClaims.PermVersion,
	}

	var token string
	var expiresAt time.Time
	token, expiresAt, err = s.codec.Sign(claims)
	if err != nil {
		return nil, err
	}

	return &AuthResponseDTO{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(s.codec.TTL().Seconds()),
		ExpiresAt:   expiresAt,
	}, nil
}

// rehashIfNeeded upgrades a password hash that predates a BCRYPT_COST increase.
func (s *AuthService) rehashIfNeeded(ctx context.Context, user *userdomain.User, password string) {
	if !s.crypt.NeedsRehash(user.PasswordHash) {
		return
	}

	var hashed string
	var err error
	hashed, err = s.crypt.HashPassword(password)
	if err != nil {
		logger.FromContext(ctx).Warn().Err(err).Msg("could not re-hash the password at the new cost")
		return
	}
	user.PasswordHash = hashed
}

// VerifyEmail confirms an address and activates the account.
func (s *AuthService) VerifyEmail(ctx context.Context, req *VerifyEmailRequestDTO) (*MessageResponseDTO, error) {
	var payload *crypt.TokenPayload
	var err error
	payload, err = s.consumeToken(ctx, req.Token, crypt.PurposeEmailVerification)
	if err != nil {
		return nil, err
	}

	var user *userdomain.User
	user, err = s.users.GetByID(ctx, payload.Subject)
	if err != nil {
		return nil, err
	}

	// Already verified: the token was valid and has now been consumed, so
	// report success rather than an error. Clicking a link twice is not a
	// failure the person can act on.
	if !user.EmailVerified() {
		var now time.Time = time.Now().UTC()
		user.EmailVerifiedAt = &now

		// Verification activates the account, but it must never *un*-suspend
		// one. An administrator's decision outranks a link in an inbox.
		if user.Status == userdomain.UserStatusInactive {
			user.Status = userdomain.UserStatusActive
		}

		err = s.users.Update(ctx, user)
		if err != nil {
			return nil, err
		}
	}

	return &MessageResponseDTO{Message: "email address verified; you can now sign in"}, nil
}

// RequestPasswordReset emails a reset link, and says the same thing either way.
//
// The response never reveals whether the identifier matched an account. That is
// the whole design of this endpoint: it is unauthenticated and accepts an
// address, so any difference in its answer is a membership oracle for whatever
// list of addresses an attacker already has.
func (s *AuthService) RequestPasswordReset(
	ctx context.Context,
	req *ForgotPasswordRequestDTO,
) (*MessageResponseDTO, error) {
	var acknowledgement *MessageResponseDTO = &MessageResponseDTO{Message: genericResetAcknowledgement}
	var log = logger.FromContext(ctx)

	var user *userdomain.User
	var err error
	user, err = s.users.FindByIdentifier(ctx, req.Identifier)
	if err != nil {
		return nil, err
	}

	if user == nil {
		log.Info().Msg("password reset requested for an unknown identifier")
		return acknowledgement, nil
	}
	if user.IsSuspended() {
		log.Info().Str("user_id", user.ID.String()).
			Msg("password reset requested for a suspended account")
		return acknowledgement, nil
	}

	// Issuing a new link retires any earlier one, so a reset mail that has been
	// sitting in a mailbox stops working the moment a fresh one is requested.
	var now time.Time = time.Now().UTC()
	err = s.tokens.InvalidateOutstanding(ctx, user.ID, crypt.PurposePasswordReset, now)
	if err != nil {
		return nil, err
	}

	var token string
	token, err = s.issueLinkToken(ctx, user, crypt.PurposePasswordReset, s.cfg.Auth.PasswordResetTokenTTL)
	if err != nil {
		return nil, err
	}

	err = s.sendMail(ctx, user, "Reset your password",
		"Use the link below to choose a new password. It can be used once, and expires in "+
			s.cfg.Auth.PasswordResetTokenTTL.String()+".\n\n"+
			s.frontendLink("reset-password", token)+
			"\n\nIf you did not ask for this, you can ignore this message; your password is unchanged.")
	if err != nil {
		log.Error().Err(err).Str("user_id", user.ID.String()).
			Msg("could not send the password reset email")
	}

	return acknowledgement, nil
}

// ResetPassword sets a new password from a reset token.
func (s *AuthService) ResetPassword(
	ctx context.Context,
	req *ResetPasswordRequestDTO,
) (*MessageResponseDTO, error) {
	var err error = userdomain.ValidatePassword(req.NewPassword)
	if err != nil {
		return nil, err
	}

	var payload *crypt.TokenPayload
	payload, err = s.consumeToken(ctx, req.Token, crypt.PurposePasswordReset)
	if err != nil {
		return nil, err
	}

	var user *userdomain.User
	user, err = s.users.GetByID(ctx, payload.Subject)
	if err != nil {
		return nil, err
	}
	if user.IsSuspended() {
		// The token was valid, but a suspended account must not be recoverable
		// by whoever holds a link.
		return nil, errors.NewForbiddenError("this account cannot be recovered; contact support", nil)
	}

	var hashed string
	hashed, err = s.crypt.HashPassword(req.NewPassword)
	if err != nil {
		return nil, err
	}
	user.PasswordHash = hashed

	// Someone who completes a reset has proven control of the address, so the
	// address is verified and the account is usable.
	if !user.EmailVerified() {
		var now time.Time = time.Now().UTC()
		user.EmailVerifiedAt = &now
	}
	if user.Status == userdomain.UserStatusInactive {
		user.Status = userdomain.UserStatusActive
	}

	err = s.users.Update(ctx, user)
	if err != nil {
		return nil, err
	}

	return &MessageResponseDTO{Message: "password updated; you can now sign in"}, nil
}

// consumeToken verifies a link token and redeems it, in that order.
//
// Both halves are needed and neither is sufficient. The signature check rejects
// forgeries without touching the database; the conditional update is what makes
// the token single-use, and it is conditional precisely so that two requests
// carrying the same link cannot both win.
func (s *AuthService) consumeToken(
	ctx context.Context,
	token string,
	purpose crypt.Purpose,
) (*crypt.TokenPayload, error) {
	var payload *crypt.TokenPayload
	var err error
	payload, err = s.crypt.VerifyToken(token, purpose)
	if err != nil {
		return nil, err
	}

	var record *userdomain.VerificationToken
	record, err = s.tokens.FindByFingerprint(ctx, crypt.Fingerprint(token))
	if err != nil {
		return nil, err
	}
	if record == nil || record.Purpose != purpose || record.UserID != payload.Subject {
		return nil, errors.NewTokenInvalidError("this link is not valid", nil)
	}

	var claimed bool
	claimed, err = s.tokens.Consume(ctx, record.ID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, errors.NewGoneError("this link has already been used; request a new one", nil)
	}

	return payload, nil
}

// issueLinkToken mints a token and records its fingerprint so it can be used
// once.
func (s *AuthService) issueLinkToken(
	ctx context.Context,
	user *userdomain.User,
	purpose crypt.Purpose,
	ttl time.Duration,
) (string, error) {
	var token string
	var payload *crypt.TokenPayload
	var err error
	token, payload, err = s.crypt.IssueToken(user.ID, purpose, ttl)
	if err != nil {
		return "", err
	}

	var record *userdomain.VerificationToken = &userdomain.VerificationToken{
		UserID:      user.ID,
		Fingerprint: crypt.Fingerprint(token),
		Purpose:     purpose,
		ExpiresAt:   time.Unix(payload.ExpiresAt, 0).UTC(),
	}

	err = s.tokens.Create(ctx, record)
	if err != nil {
		return "", err
	}
	return token, nil
}

// sendVerificationLink issues and mails an email-verification token.
func (s *AuthService) sendVerificationLink(ctx context.Context, user *userdomain.User) error {
	var token string
	var err error
	token, err = s.issueLinkToken(ctx, user, crypt.PurposeEmailVerification, s.cfg.Auth.VerificationTokenTTL)
	if err != nil {
		return err
	}

	return s.sendMail(ctx, user, "Verify your email address",
		"Welcome. Confirm this address to activate your account. The link can be used once, "+
			"and expires in "+s.cfg.Auth.VerificationTokenTTL.String()+".\n\n"+
			s.frontendLink("verify-email", token))
}

// sendMail addresses a message to a user.
func (s *AuthService) sendMail(ctx context.Context, user *userdomain.User, subject string, body string) error {
	return s.mailer.Send(ctx, mail.Message{
		To:      user.Email,
		Subject: subject,
		Text:    "Hello " + user.FullName() + ",\n\n" + body + "\n",
	})
}

// frontendLink builds the URL a recipient clicks.
//
// It points at FRONTEND_URL, not at this API: the page that collects a new
// password is part of the client application, and it calls the API itself. The
// token goes in the query string, which is why it is single-use and short-lived
// — query strings end up in browser history and referrer headers.
func (s *AuthService) frontendLink(path string, token string) string {
	var base string = s.cfg.Frontend.URL
	var query url.Values = url.Values{}
	query.Set("token", token)
	return base + "/" + path + "?" + query.Encode()
}

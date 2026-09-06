// File: internal/modules/users/service/user_service.go

package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/domain"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/crypt"
	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/shared/errors"
)

// UserService owns the account resource: reading it, and the changes an account
// holder or an administrator makes to it.
//
// The credential lifecycle — registering, logging in, verifying an address,
// resetting a forgotten password — lives in the auth module instead. The split
// is by what breaks when it changes: this service changes when the account
// *shape* does, auth changes when the *authentication rules* do.
type UserService struct {
	users       domain.UserRepository
	crypt       *crypt.Service
	suspensions domain.SuspensionRegistry
}

// NewUserService builds the service.
func NewUserService(
	users domain.UserRepository,
	cryptSvc *crypt.Service,
	suspensions domain.SuspensionRegistry,
) *UserService {
	return &UserService{users: users, crypt: cryptSvc, suspensions: suspensions}
}

// GetByID returns one account, or a NOT_FOUND AppError.
func (s *UserService) GetByID(ctx context.Context, id uuid.UUID) (*UserResponseDTO, error) {
	var user *domain.User
	var err error
	user, err = s.users.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return toUserResponse(user), nil
}

// List returns a page of accounts.
func (s *UserService) List(ctx context.Context, page domain.Page) (*UserListResponseDTO, error) {
	var normalized domain.Page = page.Normalize()

	var users []*domain.User
	var total int64
	var err error
	users, total, err = s.users.List(ctx, normalized)
	if err != nil {
		return nil, err
	}

	var response *UserListResponseDTO = &UserListResponseDTO{
		Users:  make([]*UserResponseDTO, 0, len(users)),
		Total:  total,
		Limit:  normalized.Limit,
		Offset: normalized.Offset,
	}

	var user *domain.User
	for _, user = range users {
		response.Users = append(response.Users, toUserResponse(user))
	}
	return response, nil
}

// UpdateProfile applies a partial update to the caller's own account.
//
// Only the fields present in the request are touched, and a username change is
// re-validated and checked for collision before it is written — the unique
// index would catch a duplicate anyway, but a CONFLICT naming the field beats
// a driver error translated after the fact.
func (s *UserService) UpdateProfile(
	ctx context.Context,
	id uuid.UUID,
	req *UpdateProfileRequestDTO,
) (*UserResponseDTO, error) {
	var user *domain.User
	var err error
	user, err = s.users.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Username != nil {
		var username string = domain.NormalizeUsername(*req.Username)
		err = domain.ValidateUsername(username)
		if err != nil {
			return nil, err
		}

		if username != user.Username {
			var existing *domain.User
			existing, err = s.users.FindByUsername(ctx, username)
			if err != nil {
				return nil, err
			}
			if existing != nil {
				return nil, errors.NewConflictError("that username is already taken", nil).
					WithDetail("username", "is already taken")
			}
			user.Username = username
		}
	}

	if req.FirstName != nil {
		err = domain.ValidateName("first_name", *req.FirstName)
		if err != nil {
			return nil, err
		}
		user.FirstName = *req.FirstName
	}

	if req.LastName != nil {
		err = domain.ValidateName("last_name", *req.LastName)
		if err != nil {
			return nil, err
		}
		user.LastName = *req.LastName
	}

	err = s.users.Update(ctx, user)
	if err != nil {
		return nil, err
	}
	return toUserResponse(user), nil
}

// ChangePassword replaces the caller's password, having first checked the
// current one.
//
// Requiring the current password is what stops a stolen access token from
// becoming permanent ownership of the account: the thief has the token but not
// the password, so they cannot lock the owner out.
func (s *UserService) ChangePassword(
	ctx context.Context,
	id uuid.UUID,
	req *ChangePasswordRequestDTO,
) error {
	var user *domain.User
	var err error
	user, err = s.users.GetByID(ctx, id)
	if err != nil {
		return err
	}

	err = s.crypt.ComparePassword(user.PasswordHash, req.CurrentPassword)
	if err != nil {
		if errors.IsType(err, errors.TypeInvalidCredentials) {
			return errors.NewValidationError("current password is incorrect", nil).
				WithDetail("current_password", "is incorrect")
		}
		return err
	}

	err = domain.ValidatePassword(req.NewPassword)
	if err != nil {
		return err
	}

	var hashed string
	hashed, err = s.crypt.HashPassword(req.NewPassword)
	if err != nil {
		return err
	}

	user.PasswordHash = hashed
	return s.users.Update(ctx, user)
}

// UpdateStatus is the administrative enable/disable, gated by a permission on
// the route.
//
// The row is written first and the registry second, and that order matters. If
// the registry were updated first and the write then failed, the account would
// be blocked in memory with nothing in the database to say why — and the block
// would vanish at the next restart. This way a failed write leaves both
// unchanged.
//
// The registry update is what makes a suspension immediate. Without it the
// account stays usable until its access token expires, which at the default TTL
// is a day.
func (s *UserService) UpdateStatus(
	ctx context.Context,
	id uuid.UUID,
	status domain.UserStatus,
) (*UserResponseDTO, error) {
	if !status.Valid() {
		return nil, errors.NewValidationError(
			"status must be one of active, inactive, suspended", nil).
			WithDetail("status", "must be one of active, inactive, suspended")
	}

	var user *domain.User
	var err error
	user, err = s.users.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	user.Status = status
	err = s.users.Update(ctx, user)
	if err != nil {
		return nil, err
	}

	if status == domain.UserStatusSuspended {
		s.suspensions.Suspend(user.ID)
	} else {
		s.suspensions.Restore(user.ID)
	}

	return toUserResponse(user), nil
}

// TouchLastLogin records a successful sign-in.
//
// It is here rather than in the auth service because it writes to the account,
// and it swallows nothing: a failure to record the timestamp is returned, and
// the caller decides whether a successful login should fail over it. (It should
// not — see the call site.)
func (s *UserService) TouchLastLogin(ctx context.Context, user *domain.User, at time.Time) error {
	user.LastLoginAt = &at
	return s.users.Update(ctx, user)
}

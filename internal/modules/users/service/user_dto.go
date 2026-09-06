// File: internal/modules/users/service/user_dto.go

package service

import (
	"time"

	"github.com/google/uuid"

	"github.com/DevanshSharmaT-T/Go-Gin-Template/internal/modules/users/domain"
)

// DTOs live in service/ rather than api/ for two reasons: swag resolves them as
// service.XxxDTO in the generated OpenAPI spec, and it keeps the transport
// layer from defining the contract it is supposed to be rendering.
//
// **The entity never leaves this layer.** Every response is one of these
// structs, built by an explicit mapper. That is the mechanism keeping
// PasswordHash out of responses: not a `json:"-"` tag that someone can drop,
// but a type that has no field to put it in.

// UserResponseDTO is the public representation of an account.
type UserResponseDTO struct {
	ID            uuid.UUID  `json:"id"`
	Username      string     `json:"username"`
	Email         string     `json:"email"`
	FirstName     string     `json:"first_name"`
	LastName      string     `json:"last_name"`
	FullName      string     `json:"full_name"`
	RoleID        int64      `json:"role_id"`
	Status        string     `json:"status"`
	EmailVerified bool       `json:"email_verified"`
	LastLoginAt   *time.Time `json:"last_login_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// UpdateProfileRequestDTO is a partial update of one's own account.
//
// Every field is a pointer so that "absent" and "set to empty" are different
// requests. With plain strings a PATCH that omits first_name is
// indistinguishable from one clearing it, and the handler has to guess.
//
// There is deliberately no role_id, status or email_verified field here. Those
// are not the account holder's to change, and the way that stays true is for
// the request type to have nowhere to put them.
type UpdateProfileRequestDTO struct {
	Username  *string `json:"username"`
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
}

// ChangePasswordRequestDTO changes one's own password.
//
// The current password is required even though the caller is already
// authenticated. It is what makes a stolen access token insufficient to take
// the account over permanently.
type ChangePasswordRequestDTO struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
}

// UpdateStatusRequestDTO is the administrative status change.
type UpdateStatusRequestDTO struct {
	Status string `json:"status" binding:"required"`
}

// UserListResponseDTO is one page of accounts.
type UserListResponseDTO struct {
	Users  []*UserResponseDTO `json:"users"`
	Total  int64              `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

// toUserResponse maps an entity to its public shape. It is the only place that
// mapping happens, so a field added to the entity is invisible to clients until
// somebody adds it here on purpose.
func toUserResponse(user *domain.User) *UserResponseDTO {
	if user == nil {
		return nil
	}
	return &UserResponseDTO{
		ID:            user.ID,
		Username:      user.Username,
		Email:         user.Email,
		FirstName:     user.FirstName,
		LastName:      user.LastName,
		FullName:      user.FullName(),
		RoleID:        user.RoleID,
		Status:        user.Status.String(),
		EmailVerified: user.EmailVerified(),
		LastLoginAt:   user.LastLoginAt,
		CreatedAt:     user.CreatedAt,
		UpdatedAt:     user.UpdatedAt,
	}
}

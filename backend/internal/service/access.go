package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// AccessService implements MA-4 (Check permissions): it decides whether a user, identified by id,
// may perform an action that requires a permission.
type AccessService struct {
	users UserRepository
}

func NewAccessService(users UserRepository) *AccessService {
	return &AccessService{users: users}
}

// CheckPermissions is checkPermissions(userId, requiredPermission) from the MA-4 sequence diagram.
//
//	true, nil                  access granted
//	false, domain.ErrForbidden access denied ("Access denied")
//	false, domain.ErrUnauthorized the user does not exist
func (s *AccessService) CheckPermissions(ctx context.Context, userID string, required domain.Permission) (bool, error) {
	// UserRepository.findById(userId) -> user
	user, err := s.users.FindByID(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return false, domain.ErrUnauthorized
	}
	if err != nil {
		return false, fmt.Errorf("find user: %w", err)
	}

	// user.getRole() -> role
	role := user.GetRole()
	if role == nil {
		return false, domain.ErrForbidden
	}

	// user.hasPermission(required) -> role.hasPermission(required) -> permission.equals(required)
	if !user.HasPermission(required) {
		return false, domain.ErrForbidden // accessDenied
	}
	return true, nil // accessGranted
}

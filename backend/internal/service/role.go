package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// RoleRepository is used by MA-1.
type RoleRepository interface {
	// FindDefaultRole returns the role given to every new user, with its permissions loaded,
	// or domain.ErrNotFound when none is configured.
	FindDefaultRole(ctx context.Context) (*domain.Role, error)

	// FindByID returns the role with the given id, or domain.ErrNotFound.
	FindByID(ctx context.Context, id string) (*domain.Role, error)
}

// RoleService implements MA-3 (Assign role to user).
type RoleService struct {
	roles RoleRepository
	users UserRepository
}

func NewRoleService(roles RoleRepository, users UserRepository) *RoleService {
	return &RoleService{roles: roles, users: users}
}

// AssignRole implements MA-3: assignRole(admin, targetUserId, newRoleId).
func (s *RoleService) AssignRole(ctx context.Context, admin *domain.User, targetUserID, newRoleID string) (*domain.User, error) {
	if admin == nil {
		return nil, domain.ErrUnauthorized
	}
	// admin.isPrivileged() -> true, admin.hasPermission(MANAGE_ROLES) -> adminRole.hasPermission(MANAGE_ROLES) -> true
	if !admin.IsPrivileged() || !admin.HasPermission(domain.ManageRoles) {
		return nil, domain.ErrForbidden
	}

	targetUserID = strings.TrimSpace(targetUserID)
	newRoleID = strings.TrimSpace(newRoleID)
	problems := map[string]string{}
	if targetUserID == "" {
		problems["targetUserId"] = "is required"
	}
	if newRoleID == "" {
		problems["newRoleId"] = "is required"
	}
	if len(problems) > 0 {
		return nil, &domain.ValidationError{Fields: problems}
	}

	// UserRepository.findById(targetUserId) -> targetUser
	targetUser, err := s.users.FindByID(ctx, targetUserID)
	if err != nil {
		return nil, fmt.Errorf("find target user: %w", err)
	}

	// RoleRepository.findById(newRoleId) -> newRole
	newRole, err := s.roles.FindByID(ctx, newRoleID)
	if err != nil {
		return nil, fmt.Errorf("find role: %w", err)
	}

	// targetUser.setRole(newRole)
	targetUser.SetRole(newRole)

	// UserRepository.save(targetUser) -> updatedUser
	updatedUser, err := s.users.Save(ctx, targetUser)
	if err != nil {
		return nil, fmt.Errorf("save user: %w", err)
	}
	return updatedUser, nil
}

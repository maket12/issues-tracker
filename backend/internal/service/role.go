package service

import (
	"context"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// RoleRepository is used by MA-1.
type RoleRepository interface {
	// FindDefaultRole returns the role given to every new user, with its permissions loaded,
	// or domain.ErrNotFound when none is configured.
	FindDefaultRole(ctx context.Context) (*domain.Role, error)
}
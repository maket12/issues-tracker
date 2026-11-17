package service

import (
	"context"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// SessionRepository is used by MA-2 (Log in / Log out).
type SessionRepository interface {
	SaveSession(ctx context.Context, user *domain.User, role *domain.Role) (*domain.Session, error)
	DeleteByUserID(ctx context.Context, userID string) (bool, error)
}

package service

import (
	"context"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// UserRepository is used by MA-1.
type UserRepository interface {
	// ExistsByEmail expects an already normalised (trimmed, lower-case) e-mail.
	ExistsByEmail(ctx context.Context, email string) (bool, error)

	// Save stores a new user, assigns its id and returns it as stored.
	// It returns domain.ErrEmailTaken when the e-mail is already registered (unique index),
	// which can still happen when two registrations race.
	Save(ctx context.Context, user *domain.User) (*domain.User, error)
}
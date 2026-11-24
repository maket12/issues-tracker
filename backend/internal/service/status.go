package service

import (
	"context"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// StatusRepository is used by LC-1 (define statuses) and LC-2 (define transitions).
type StatusRepository interface {
	// FindByID returns the status with the given id, or domain.ErrNotFound.
	FindByID(ctx context.Context, id string) (*domain.Status, error)

	// Save stores a status. For a new status (empty id) it assigns the id to the given object, so the
	// status that is already part of the lifecycle becomes the stored one, and returns it as stored.
	// It returns domain.ErrStatusExists when the name is already taken.
	Save(ctx context.Context, status *domain.Status) (*domain.Status, error)
}

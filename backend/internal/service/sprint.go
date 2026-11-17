package service

import (
	"context"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// SprintRepository is used by IM-5.
type SprintRepository interface {
	// FindByIssue returns the sprint the issue belongs to (an issue is in at most one sprint),
	// or domain.ErrNotFound when it is in no sprint.
	FindByIssue(ctx context.Context, issue *domain.Issue) (*domain.Sprint, error)

	// Save stores the sprint together with its current list of issues and returns it as stored.
	Save(ctx context.Context, sprint *domain.Sprint) (*domain.Sprint, error)
}

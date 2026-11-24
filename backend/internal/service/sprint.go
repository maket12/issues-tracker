package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// SprintRepository is used by IM-5, WO-1, WO-2 and WO-4.
type SprintRepository interface {
	// FindByID returns the sprint with the given id, or domain.ErrNotFound.
	FindByID(ctx context.Context, id string) (*domain.Sprint, error)

	// FindByIssue returns the sprint the issue belongs to (an issue is in at most one sprint),
	// or domain.ErrNotFound when it is in no sprint.
	FindByIssue(ctx context.Context, issue *domain.Issue) (*domain.Sprint, error)

	// Save stores the sprint together with its current list of issues and returns it as stored.
	Save(ctx context.Context, sprint *domain.Sprint) (*domain.Sprint, error)
}

// SprintService implements WO-1 (Create sprint) and WO-2 (Link issue to sprint).
type SprintService struct {
	sprints SprintRepository
	issues  IssueRepository
}

func NewSprintService(sprints SprintRepository, issues IssueRepository) *SprintService {
	return &SprintService{sprints: sprints, issues: issues}
}

// CreateSprint implements WO-1: createSprint(name, startDate, endDate, user).
func (s *SprintService) CreateSprint(ctx context.Context, name string, startDate, endDate domain.Date, user *domain.User) (*domain.Sprint, error) {
	if user == nil {
		return nil, domain.ErrUnauthorized
	}
	// user.hasPermission(MANAGE_SPRINTS) -> role.hasPermission(MANAGE_SPRINTS)
	if !user.HasPermission(domain.ManageSprints) {
		return nil, domain.ErrForbidden
	}

	// new Sprint(name, startDate, endDate)
	sprint, err := domain.NewSprint(name, startDate, endDate)
	if err != nil {
		return nil, err
	}

	// sprint.getIssues() -> emptyList
	_ = sprint.GetIssues()

	// SprintRepository.save(sprint) -> savedSprint
	saved, err := s.sprints.Save(ctx, sprint)
	if err != nil {
		return nil, fmt.Errorf("save sprint: %w", err)
	}
	return saved, nil
}

// LinkIssueToSprint implements WO-2: linkIssueToSprint(sprintId, issueId, user).
func (s *SprintService) LinkIssueToSprint(ctx context.Context, sprintID, issueID string, user *domain.User) (*domain.Sprint, error) {
	if user == nil {
		return nil, domain.ErrUnauthorized
	}
	// user.hasPermission(MANAGE_SPRINTS) -> role.hasPermission(MANAGE_SPRINTS)
	if !user.HasPermission(domain.ManageSprints) {
		return nil, domain.ErrForbidden
	}

	sprintID = strings.TrimSpace(sprintID)
	issueID = strings.TrimSpace(issueID)
	problems := map[string]string{}
	if sprintID == "" {
		problems["sprintId"] = "is required"
	}
	if issueID == "" {
		problems["issueId"] = "is required"
	}
	if len(problems) > 0 {
		return nil, &domain.ValidationError{Fields: problems}
	}

	// SprintRepository.findById(sprintId) -> sprint
	sprint, err := s.sprints.FindByID(ctx, sprintID)
	if err != nil {
		return nil, fmt.Errorf("find sprint: %w", err)
	}
	if sprint.IsClosed() {
		return nil, domain.ErrSprintClosed
	}

	// IssueRepository.findById(issueId) -> issue
	issue, err := s.issues.FindByID(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("find issue: %w", err)
	}

	// sprint.contains(issue) -> false
	if sprint.Contains(issue) {
		return nil, domain.ErrIssueAlreadyInSprint
	}

	// sprint.addIssue(issue)
	sprint.AddIssue(issue)

	// SprintRepository.save(sprint) -> updatedSprint
	updated, err := s.sprints.Save(ctx, sprint)
	if err != nil {
		return nil, fmt.Errorf("save sprint: %w", err)
	}
	return updated, nil
}

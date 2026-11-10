package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

type IssueRepository interface {
	FindByID(ctx context.Context, id string) (*domain.Issue, error)
	FindTypeByID(ctx context.Context, id string) (*domain.IssueType, error)
	FindPriorityByID(ctx context.Context, id string) (*domain.Priority, error)
	FindUserByID(ctx context.Context, id string) (*domain.User, error)
	FindInitialStatus(ctx context.Context) (*domain.Status, error)
	Save(ctx context.Context, issue *domain.Issue) (*domain.Issue, error)
}

type AttributeData struct {
	Name        string
	TypeID      string
	Description *string
	AssigneeID  *string // nil or blank: unassigned
	PriorityID  string
	StartDate   *domain.Date
	DueDate     *domain.Date
}

type IssueService struct {
	repo IssueRepository
	now  func() time.Time
}

func NewIssueService(repo IssueRepository) *IssueService {
	return &IssueService{repo: repo, now: time.Now}
}

func (s *IssueService) CreateIssue(ctx context.Context, name, typeID string, description *string, author *domain.User) (*domain.Issue, error) {
	if author == nil {
		return nil, domain.ErrUnauthorized
	}
	// author.hasPermission(CREATE_ISSUE) -> role.hasPermission(CREATE_ISSUE)
	if !author.HasPermission(domain.CreateIssue) {
		return nil, domain.ErrForbidden
	}

	typeID = strings.TrimSpace(typeID)
	if typeID == "" {
		return nil, domain.NewValidation("typeId", "is required")
	}
	issueType, err := s.repo.FindTypeByID(ctx, typeID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.NewValidation("typeId", fmt.Sprintf("unknown issue type %q", typeID))
	}
	if err != nil {
		return nil, fmt.Errorf("find issue type: %w", err)
	}

	status, err := s.repo.FindInitialStatus(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrNoInitialStatus
	}
	if err != nil {
		return nil, fmt.Errorf("find initial status: %w", err)
	}

	issue, err := domain.NewIssue(name, issueType, description, author, status, s.now())
	if err != nil {
		return nil, err
	}
	saved, err := s.repo.Save(ctx, issue)
	if err != nil {
		return nil, fmt.Errorf("save issue: %w", err)
	}
	return saved, nil
}

func (s *IssueService) FillAttributes(ctx context.Context, issueID string, attr AttributeData, user *domain.User) (*domain.Issue, error) {
	if user == nil {
		return nil, domain.ErrUnauthorized
	}
	issue, err := s.repo.FindByID(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("find issue: %w", err)
	}
	if !issue.IsAccessibleBy(user) {
		return nil, domain.ErrForbidden
	}

	problems := map[string]string{}
	var (
		issueType *domain.IssueType
		priority  *domain.Priority
		assignee  *domain.User
	)
	if id := strings.TrimSpace(attr.TypeID); id == "" {
		problems["typeId"] = "is required"
	} else if issueType, err = lookup(ctx, problems, "typeId", "issue type", id, s.repo.FindTypeByID); err != nil {
		return nil, err
	}
	if id := strings.TrimSpace(attr.PriorityID); id == "" {
		problems["priorityId"] = "is required"
	} else if priority, err = lookup(ctx, problems, "priorityId", "priority", id, s.repo.FindPriorityByID); err != nil {
		return nil, err
	}
	if attr.AssigneeID != nil {
		if id := strings.TrimSpace(*attr.AssigneeID); id != "" {
			if assignee, err = lookup(ctx, problems, "assigneeId", "user", id, s.repo.FindUserByID); err != nil {
				return nil, err
			}
		}
	}
	if attr.StartDate == nil {
		problems["startDate"] = "is required"
	}
	if len(problems) > 0 {
		return nil, &domain.ValidationError{Fields: problems}
	}

	if err := issue.Edit(attr.Name, issueType, attr.Description, assignee, priority, *attr.StartDate, attr.DueDate); err != nil {
		return nil, err
	}
	updated, err := s.repo.Save(ctx, issue)
	if err != nil {
		return nil, fmt.Errorf("save issue: %w", err)
	}
	return updated, nil
}

func lookup[T any](ctx context.Context, problems map[string]string, field, label, id string,
	find func(context.Context, string) (*T, error)) (*T, error) {

	v, err := find(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		problems[field] = fmt.Sprintf("unknown %s %q", label, id)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", label, err)
	}
	return v, nil
}

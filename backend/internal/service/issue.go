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
	FindStatusByID(ctx context.Context, id string) (*domain.Status, error)
	Save(ctx context.Context, issue *domain.Issue) (*domain.Issue, error)
	Delete(ctx context.Context, issue *domain.Issue) (bool, error)
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
	repo       IssueRepository
	sprints    SprintRepository
	lifecycles LifecycleRepository
	now        func() time.Time
}

func NewIssueService(repo IssueRepository, sprints SprintRepository, lifecycles LifecycleRepository) *IssueService {
	return &IssueService{repo: repo, sprints: sprints, lifecycles: lifecycles, now: time.Now}
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

	return s.applyAttributes(ctx, issue, attr)
}

// ViewIssue implements IM-3: View issue.
func (s *IssueService) ViewIssue(ctx context.Context, issueID string, user *domain.User) (*domain.Issue, error) {
	if user == nil {
		return nil, domain.ErrUnauthorized
	}
	// user.hasPermission(VIEW_ISSUE) -> role.hasPermission(VIEW_ISSUE)
	if !user.HasPermission(domain.ViewIssue) {
		return nil, domain.ErrForbidden
	}

	issue, err := s.repo.FindByID(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("find issue: %w", err)
	}
	if !issue.IsAccessibleBy(user) {
		return nil, domain.ErrForbidden
	}

	return issue, nil
}

// EditIssue implements IM-4 (edit issue) and MA-5 (work simultaneously):
// editIssue(issueId, changes, expectedVersion, user).
//
// expectedVersion is the version of the issue the user was looking at when they started editing. If the
// issue was saved by someone else in the meantime, its version differs and the edit is rejected with
// domain.ErrConflict ("refresh required"): the later save never overwrites the earlier one.
func (s *IssueService) EditIssue(ctx context.Context, issueID string, changes AttributeData, expectedVersion string, user *domain.User) (*domain.Issue, error) {
	if user == nil {
		return nil, domain.ErrUnauthorized
	}
	// user.hasPermission(EDIT_ISSUE) -> role.hasPermission(EDIT_ISSUE)
	if !user.HasPermission(domain.EditIssue) {
		return nil, domain.ErrForbidden
	}

	// IssueRepository.findById(issueId) -> issue
	issue, err := s.repo.FindByID(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("find issue: %w", err)
	}
	// issue.isAccessibleBy(user) -> true
	if !issue.IsAccessibleBy(user) {
		return nil, domain.ErrForbidden
	}

	// issue.getVersion() -> currentVersion; [expectedVersion != currentVersion] -> conflictDetected
	expectedVersion = strings.TrimSpace(expectedVersion)
	if expectedVersion == "" {
		return nil, domain.NewValidation("version", "is required: send the version you loaded")
	}
	if expectedVersion != issue.GetVersion() {
		return nil, domain.ErrConflict
	}

	// issue.edit(changes), IssueRepository.save(issue): the repository saves only if nobody saved in between
	return s.applyAttributes(ctx, issue, changes)
}

// ChangeIssueStatus implements LC-4 (change issue status) and LC-5 (verify transition right):
// changeIssueStatus(issueId, targetStatusId, user).
//
// Only the transition rule decides: no rule for current -> target means domain.ErrTransitionNotAllowed,
// a rule the user does not satisfy means domain.ErrForbidden. The save is guarded by the issue's version,
// so a concurrent change of the issue makes this one fail with domain.ErrConflict.
func (s *IssueService) ChangeIssueStatus(ctx context.Context, issueID, targetStatusID string, user *domain.User) (*domain.Issue, error) {
	if user == nil {
		return nil, domain.ErrUnauthorized
	}

	// IssueRepository.findById(issueId) -> issue
	issue, err := s.repo.FindByID(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("find issue: %w", err)
	}

	// IssueRepository.findStatusById(targetStatusId) -> targetStatus
	targetStatusID = strings.TrimSpace(targetStatusID)
	if targetStatusID == "" {
		return nil, domain.NewValidation("statusId", "is required")
	}
	targetStatus, err := s.repo.FindStatusByID(ctx, targetStatusID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.NewValidation("statusId", fmt.Sprintf("unknown status %q", targetStatusID))
	}
	if err != nil {
		return nil, fmt.Errorf("find status: %w", err)
	}

	// LifecycleRepository.getLifecycle() -> lifecycle
	lifecycle, err := s.lifecycles.GetLifecycle(ctx)
	if err != nil {
		return nil, fmt.Errorf("get lifecycle: %w", err)
	}

	// lifecycle.checkTransition(issue, user, targetStatus) -> true
	if _, err := lifecycle.CheckTransition(issue, user, targetStatus); err != nil {
		return nil, err
	}

	// issue.changeStatus(targetStatus), IssueRepository.save(issue) -> updatedIssue
	if err := issue.ChangeStatus(targetStatus); err != nil {
		return nil, err
	}
	updated, err := s.repo.Save(ctx, issue)
	if err != nil {
		return nil, fmt.Errorf("save issue: %w", err)
	}
	return updated, nil
}

// DeleteIssue implements IM-5: deleteIssue(issueId, user). The returned bool is the confirmation.
func (s *IssueService) DeleteIssue(ctx context.Context, issueID string, user *domain.User) (bool, error) {
	if user == nil {
		return false, domain.ErrUnauthorized
	}
	// user.hasPermission(DELETE_ISSUE) -> role.hasPermission(DELETE_ISSUE)
	if !user.HasPermission(domain.DeleteIssue) {
		return false, domain.ErrForbidden
	}

	// IssueRepository.findById(issueId) -> issue
	issue, err := s.repo.FindByID(ctx, issueID)
	if err != nil {
		return false, fmt.Errorf("find issue: %w", err)
	}

	// SprintRepository.findByIssue(issue) -> sprint
	sprint, err := s.sprints.FindByIssue(ctx, issue)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		// the issue belongs to no sprint: nothing to detach
	case err != nil:
		return false, fmt.Errorf("find sprint: %w", err)
	case sprint != nil && sprint.Contains(issue):
		// [issue belongs to a sprint]: sprint.removeIssue(issue), SprintRepository.save(sprint)
		sprint.RemoveIssue(issue)
		if _, err := s.sprints.Save(ctx, sprint); err != nil {
			return false, fmt.Errorf("save sprint: %w", err)
		}
	}

	// IssueRepository.delete(issue) -> deleted
	deleted, err := s.repo.Delete(ctx, issue)
	if err != nil {
		return false, fmt.Errorf("delete issue: %w", err)
	}
	if !deleted { // somebody else removed it in the meantime
		return false, domain.ErrNotFound
	}
	return true, nil
}

func (s *IssueService) applyAttributes(ctx context.Context, issue *domain.Issue, attr AttributeData) (*domain.Issue, error) {
	problems := map[string]string{}
	var (
		issueType *domain.IssueType
		priority  *domain.Priority
		assignee  *domain.User
		err       error
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

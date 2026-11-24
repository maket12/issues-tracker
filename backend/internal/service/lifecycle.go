package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// LifecycleRepository stores the status graph. There is only one lifecycle: it holds every status and
// every transition.
type LifecycleRepository interface {
	GetLifecycle(ctx context.Context) (*domain.Lifecycle, error)

	// Save stores the transitions of the lifecycle with their roles and allowAuthor flags and returns the
	// lifecycle as stored. Statuses are stored by StatusRepository and must already exist. Saving only
	// adds and updates (no use case removes a transition or a role), so two administrators who work at
	// the same time do not erase each other's additions.
	Save(ctx context.Context, lifecycle *domain.Lifecycle) (*domain.Lifecycle, error)
}

// LifecycleService implements LC-1 (define statuses), LC-2 (define allowed transitions) and
// LC-3 (set transition rules). Changing an issue's status (LC-4, LC-5) is IssueService.ChangeIssueStatus.
type LifecycleService struct {
	lifecycles LifecycleRepository
	statuses   StatusRepository
	roles      RoleRepository
}

func NewLifecycleService(lifecycles LifecycleRepository, statuses StatusRepository, roles RoleRepository) *LifecycleService {
	return &LifecycleService{lifecycles: lifecycles, statuses: statuses, roles: roles}
}

// DefineStatus implements LC-1: defineStatus(statusName, user).
func (s *LifecycleService) DefineStatus(ctx context.Context, statusName string, user *domain.User) (*domain.Status, error) {
	if err := requireLifecycleRights(user); err != nil {
		return nil, err
	}

	// LifecycleRepository.getLifecycle() -> lifecycle
	lifecycle, err := s.lifecycles.GetLifecycle(ctx)
	if err != nil {
		return nil, fmt.Errorf("get lifecycle: %w", err)
	}

	// new Status(statusName), newStatus.getName() -> statusName
	newStatus, err := domain.NewStatus(statusName)
	if err != nil {
		return nil, err
	}
	name := newStatus.GetName()

	// lifecycle.addStatus(newStatus): rejects a duplicated name
	if err := lifecycle.AddStatus(newStatus); err != nil {
		return nil, err
	}

	// StatusRepository.save(newStatus) -> savedStatus
	savedStatus, err := s.statuses.Save(ctx, newStatus)
	if errors.Is(err, domain.ErrStatusExists) { // taken by someone else after the lifecycle was read
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("save status %q: %w", name, err)
	}

	// LifecycleRepository.save(lifecycle) -> updatedLifecycle
	if _, err := s.lifecycles.Save(ctx, lifecycle); err != nil {
		return nil, fmt.Errorf("save lifecycle: %w", err)
	}
	return savedStatus, nil
}

// VerifyTransition implements LC-5: checkTransition(issue, user, targetStatus).
func (s *LifecycleService) VerifyTransition(ctx context.Context, issue *domain.Issue, user *domain.User, targetStatus *domain.Status) (bool, error) {
	if issue == nil {
		return false, domain.NewValidation("issue", "is required")
	}
	if targetStatus == nil {
		return false, domain.NewValidation("targetStatus", "is required")
	}

	lifecycle, err := s.lifecycles.GetLifecycle(ctx)
	if err != nil {
		return false, fmt.Errorf("get lifecycle: %w", err)
	}
	return lifecycle.CheckTransition(issue, user, targetStatus)
}

// DefineTransition implements LC-2: defineTransition(fromId, toId, user).
func (s *LifecycleService) DefineTransition(ctx context.Context, fromID, toID string, user *domain.User) (*domain.TransitionRule, error) {
	if err := requireLifecycleRights(user); err != nil {
		return nil, err
	}

	// StatusRepository.findById(fromId) -> fromStatus, findById(toId) -> toStatus
	problems := map[string]string{}
	fromStatus, err := s.findStatus(ctx, problems, "fromId", fromID)
	if err != nil {
		return nil, err
	}
	toStatus, err := s.findStatus(ctx, problems, "toId", toID)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, &domain.ValidationError{Fields: problems}
	}

	// LifecycleRepository.getLifecycle() -> lifecycle
	lifecycle, err := s.lifecycles.GetLifecycle(ctx)
	if err != nil {
		return nil, fmt.Errorf("get lifecycle: %w", err)
	}

	// lifecycle.findRule(fromStatus, toStatus) -> null
	if lifecycle.FindRule(fromStatus, toStatus) != nil {
		return nil, domain.ErrTransitionExists
	}

	// new TransitionRule(fromStatus, toStatus), lifecycle.addTransitionRule(rule)
	rule, err := domain.NewTransitionRule(fromStatus, toStatus)
	if err != nil {
		return nil, err
	}
	if err := lifecycle.AddTransitionRule(rule); err != nil {
		return nil, err
	}

	// LifecycleRepository.save(lifecycle) -> updatedLifecycle
	updated, err := s.lifecycles.Save(ctx, lifecycle)
	if err != nil {
		return nil, fmt.Errorf("save lifecycle: %w", err)
	}
	return storedRule(updated, fromStatus, toStatus, rule), nil
}

// SetTransitionRules implements LC-3: setTransitionRules(fromStatus, toStatus, roleId, allowAuthor, user).
// The role is added to the roles allowed to make the transition; allowAuthor tells whether the author of
// an issue may make it regardless of role.
func (s *LifecycleService) SetTransitionRules(ctx context.Context, fromStatus, toStatus *domain.Status,
	roleID string, allowAuthor bool, user *domain.User) (*domain.TransitionRule, error) {

	if err := requireLifecycleRights(user); err != nil {
		return nil, err
	}

	// LifecycleRepository.getLifecycle() -> lifecycle
	lifecycle, err := s.lifecycles.GetLifecycle(ctx)
	if err != nil {
		return nil, fmt.Errorf("get lifecycle: %w", err)
	}

	// lifecycle.findRule(fromStatus, toStatus) -> rule (rule.matches(...) is asked inside)
	rule := lifecycle.FindRule(fromStatus, toStatus)
	if rule == nil {
		return nil, fmt.Errorf("transition %s -> %s: %w", fromStatus.GetName(), toStatus.GetName(), domain.ErrNotFound)
	}

	// RoleRepository.findById(roleId) -> allowedRole
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return nil, domain.NewValidation("roleId", "is required")
	}
	allowedRole, err := s.roles.FindByID(ctx, roleID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.NewValidation("roleId", fmt.Sprintf("unknown role %q", roleID))
	}
	if err != nil {
		return nil, fmt.Errorf("find role: %w", err)
	}

	// rule.addRole(allowedRole), lifecycle.setAllowedRole(allowedRole)
	rule.AddRole(allowedRole)
	rule.SetAllowAuthor(allowAuthor)
	lifecycle.SetAllowedRole(allowedRole)

	// LifecycleRepository.save(lifecycle) -> savedLifecycle
	saved, err := s.lifecycles.Save(ctx, lifecycle)
	if err != nil {
		return nil, fmt.Errorf("save lifecycle: %w", err)
	}
	return storedRule(saved, fromStatus, toStatus, rule), nil
}

// SetTransitionRulesByID is SetTransitionRules for callers that only know status ids (HTTP): the statuses
// are looked up first, as in LC-2.
func (s *LifecycleService) SetTransitionRulesByID(ctx context.Context, fromID, toID, roleID string,
	allowAuthor bool, user *domain.User) (*domain.TransitionRule, error) {

	if err := requireLifecycleRights(user); err != nil {
		return nil, err
	}
	problems := map[string]string{}
	fromStatus, err := s.findStatus(ctx, problems, "fromId", fromID)
	if err != nil {
		return nil, err
	}
	toStatus, err := s.findStatus(ctx, problems, "toId", toID)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, &domain.ValidationError{Fields: problems}
	}
	return s.SetTransitionRules(ctx, fromStatus, toStatus, roleID, allowAuthor, user)
}

// requireLifecycleRights: user.hasPermission(CONFIGURE_LIFECYCLE) -> role.hasPermission(CONFIGURE_LIFECYCLE)
func requireLifecycleRights(user *domain.User) error {
	if user == nil {
		return domain.ErrUnauthorized
	}
	if !user.HasPermission(domain.ConfigureLifecycle) {
		return domain.ErrForbidden
	}
	return nil
}

// findStatus looks a status up by id; a missing or unknown id becomes a validation problem.
func (s *LifecycleService) findStatus(ctx context.Context, problems map[string]string, field, id string) (*domain.Status, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		problems[field] = "is required"
		return nil, nil
	}
	status, err := s.statuses.FindByID(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		problems[field] = fmt.Sprintf("unknown status %q", id)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find status: %w", err)
	}
	return status, nil
}

// storedRule returns the rule as it was stored, falling back to the one that was saved.
func storedRule(saved *domain.Lifecycle, from, to *domain.Status, fallback *domain.TransitionRule) *domain.TransitionRule {
	if saved != nil {
		if rule := saved.FindRule(from, to); rule != nil {
			return rule
		}
	}
	return fallback
}

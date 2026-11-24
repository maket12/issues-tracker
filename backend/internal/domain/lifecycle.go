package domain

import (
	"errors"
	"slices"
	"strings"
)

// Lifecycle is the status graph of all issues: the statuses and the transitions allowed between them.
type Lifecycle struct {
	statuses     []*Status
	rules        []*TransitionRule
	allowedRoles []*Role
}

func NewLifecycle() *Lifecycle { return &Lifecycle{} }

// RestoreLifecycle rebuilds the stored lifecycle; LifecycleRepository implementations use it.
func RestoreLifecycle(statuses []*Status, rules []*TransitionRule) *Lifecycle {
	l := &Lifecycle{statuses: slices.Clone(statuses), rules: slices.Clone(rules)}
	for _, rule := range rules {
		for _, role := range rule.GetAllowedRoles() {
			l.SetAllowedRole(role)
		}
	}
	return l
}

func (l *Lifecycle) GetStatuses() []*Status { return slices.Clone(l.statuses) }

func (l *Lifecycle) GetRules() []*TransitionRule { return slices.Clone(l.rules) }

// GetAllowedRoles returns the roles known to the lifecycle: those registered with SetAllowedRole
// and those that appear in any rule.
func (l *Lifecycle) GetAllowedRoles() []*Role { return slices.Clone(l.allowedRoles) }

// AddStatus registers a status. Names must be unique (compared case-insensitively).
func (l *Lifecycle) AddStatus(status *Status) error {
	if status == nil {
		return NewValidation("name", "is required")
	}
	for _, existing := range l.statuses {
		if strings.EqualFold(strings.TrimSpace(existing.Name), strings.TrimSpace(status.Name)) {
			return ErrStatusExists
		}
	}
	l.statuses = append(l.statuses, status)
	return nil
}

// AddTransitionRule adds a transition; a second rule for the same pair of statuses is refused.
func (l *Lifecycle) AddTransitionRule(rule *TransitionRule) error {
	if rule == nil {
		return NewValidation("transition", "is required")
	}
	if l.FindRule(rule.From, rule.To) != nil {
		return ErrTransitionExists
	}
	l.rules = append(l.rules, rule)
	return nil
}

// SetAllowedRole registers a role as one of the roles the lifecycle's rules refer to.
// It is bookkeeping only: the roles of each transition are kept (and stored) by its TransitionRule.
func (l *Lifecycle) SetAllowedRole(role *Role) {
	if role == nil {
		return
	}
	if slices.ContainsFunc(l.allowedRoles, func(r *Role) bool { return r != nil && r.ID == role.ID }) {
		return
	}
	l.allowedRoles = append(l.allowedRoles, role)
}

// FindRule returns the rule for the transition from -> to, or nil when there is none.
func (l *Lifecycle) FindRule(from, to *Status) *TransitionRule {
	for _, rule := range l.rules {
		if rule.Matches(from, to) {
			return rule
		}
	}
	return nil
}

// CheckTransition implements LC-5 (verify transition right): may the user move the issue from its
// current status to the target status?
//
//	true, nil                          the transition is allowed
//	false, ErrTransitionNotAllowed     no such transition exists in the graph
//	false, ErrForbidden                the transition exists, but the user does not satisfy its rule
func (l *Lifecycle) CheckTransition(issue *Issue, user *User, target *Status) (bool, error) {
	current := issue.GetStatus() // issue.getStatus() -> currentStatus
	if current == nil {
		return false, errors.New("domain: the issue has no status")
	}
	rule := l.FindRule(current, target) // lifecycle.findRule(currentStatus, targetStatus), rule.matches(...)
	if rule == nil {
		return false, ErrTransitionNotAllowed
	}
	if !rule.IsAllowedFor(user, issue) { // rule.isAllowedFor(user, issue)
		return false, ErrForbidden
	}
	return true, nil
}

package domain

import "slices"

// TransitionRule is an allowed transition between two statuses together with the condition for using it:
// the issue's author (when allowAuthor is set) or a user holding one of the allowed roles.
// A rule without roles and without allowAuthor lets nobody perform the transition.
type TransitionRule struct {
	From         *Status
	To           *Status
	allowAuthor  bool
	allowedRoles []*Role
}

// NewTransitionRule is `new TransitionRule(fromStatus, toStatus)` from the LC-2 sequence diagram.
func NewTransitionRule(from, to *Status) (*TransitionRule, error) {
	problems := map[string]string{}
	if from == nil {
		problems["fromId"] = "is required"
	}
	if to == nil {
		problems["toId"] = "is required"
	}
	if from != nil && to != nil && from.ID == to.ID {
		problems["toId"] = "must differ from fromId"
	}
	if len(problems) > 0 {
		return nil, &ValidationError{Fields: problems}
	}
	return &TransitionRule{From: from, To: to}, nil
}

// RestoreTransitionRule rebuilds a stored rule; LifecycleRepository implementations use it.
func RestoreTransitionRule(from, to *Status, allowAuthor bool, roles []*Role) *TransitionRule {
	r := &TransitionRule{From: from, To: to, allowAuthor: allowAuthor}
	for _, role := range roles {
		r.AddRole(role)
	}
	return r
}

// Matches reports whether the rule describes the transition from -> to (statuses are compared by id).
func (r *TransitionRule) Matches(from, to *Status) bool {
	return r != nil && sameStatus(r.From, from) && sameStatus(r.To, to)
}

func sameStatus(a, b *Status) bool {
	return a != nil && b != nil && a.ID != "" && a.ID == b.ID
}

// AddRole allows the role to perform the transition; adding a role twice has no effect.
func (r *TransitionRule) AddRole(role *Role) {
	if r == nil || role == nil || r.hasRole(role) {
		return
	}
	r.allowedRoles = append(r.allowedRoles, role)
}

func (r *TransitionRule) RemoveRole(role *Role) {
	if r == nil || role == nil {
		return
	}
	r.allowedRoles = slices.DeleteFunc(r.allowedRoles, func(candidate *Role) bool {
		return candidate != nil && candidate.ID == role.ID
	})
}

func (r *TransitionRule) hasRole(role *Role) bool {
	return slices.ContainsFunc(r.allowedRoles, func(candidate *Role) bool {
		return candidate != nil && candidate.ID == role.ID
	})
}

func (r *TransitionRule) SetAllowAuthor(allow bool) {
	if r != nil {
		r.allowAuthor = allow
	}
}

func (r *TransitionRule) AllowsAuthor() bool { return r != nil && r.allowAuthor }

// GetAllowedRoles returns a copy of the allowed roles.
func (r *TransitionRule) GetAllowedRoles() []*Role {
	if r == nil {
		return nil
	}
	return slices.Clone(r.allowedRoles)
}

// IsAllowedFor is `rule.isAllowedFor(user, issue)` from the LC-4 sequence diagram.
func (r *TransitionRule) IsAllowedFor(user *User, issue *Issue) bool {
	if r == nil || user == nil {
		return false
	}
	if r.allowAuthor && user.ID != "" && issue != nil && issue.Author != nil && issue.Author.ID == user.ID {
		return true
	}
	if user.Role == nil {
		return false
	}
	return r.hasRole(user.Role)
}

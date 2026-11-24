package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------- MA-4: permission / role

func TestPermissionEqualsAndRoleHasPermission(t *testing.T) {
	assert.True(t, CreateIssue.Equals(CreateIssue))
	assert.False(t, CreateIssue.Equals(DeleteIssue))

	role := &Role{Permissions: []Permission{CreateIssue, ViewIssue}}
	assert.True(t, role.HasPermission(ViewIssue))
	assert.False(t, role.HasPermission(DeleteIssue))
	assert.False(t, (&Role{}).HasPermission(ViewIssue), "a role without permissions grants nothing")
	assert.False(t, (*Role)(nil).HasPermission(ViewIssue))
}

// ---------------------------------------------------------------- MA-5: issue version

func TestIssue_GetVersionAndStatus(t *testing.T) {
	issue := &Issue{Version: "v7", Status: &Status{ID: "open"}}
	assert.Equal(t, "v7", issue.GetVersion())
	assert.Equal(t, "open", issue.GetStatus().ID)
	assert.Empty(t, (*Issue)(nil).GetVersion())
	assert.Nil(t, (*Issue)(nil).GetStatus())
}

// ---------------------------------------------------------------- LC-1: statuses

func TestNewStatus(t *testing.T) {
	s, err := NewStatus("  In Review ")
	require.NoError(t, err)
	assert.Equal(t, "In Review", s.GetName())
	assert.Empty(t, s.ID, "the id is assigned on save")

	for _, bad := range []string{"", "   ", strings.Repeat("я", MaxStatusNameLength+1)} {
		_, err := NewStatus(bad)
		var ve *ValidationError
		require.ErrorAs(t, err, &ve, "name %q", bad)
		assert.Contains(t, ve.Fields, "name")
	}
	_, err = NewStatus(strings.Repeat("я", MaxStatusNameLength))
	assert.NoError(t, err, "100 characters (200 bytes) are allowed")
	assert.Empty(t, (*Status)(nil).GetName())
}

func TestLifecycle_AddStatusRejectsDuplicateNames(t *testing.T) {
	l := NewLifecycle()
	require.NoError(t, l.AddStatus(&Status{ID: "open", Name: "Open"}))

	assert.ErrorIs(t, l.AddStatus(&Status{Name: "Open"}), ErrStatusExists)
	assert.ErrorIs(t, l.AddStatus(&Status{Name: "  oPEN "}), ErrStatusExists, "case and surrounding spaces do not matter")
	assert.Error(t, l.AddStatus(nil))
	assert.Len(t, l.GetStatuses(), 1, "rejected statuses are not added")

	require.NoError(t, l.AddStatus(&Status{Name: "Done"}))
	assert.Len(t, l.GetStatuses(), 2)
}

// ---------------------------------------------------------------- LC-2: transitions

func TestNewTransitionRule(t *testing.T) {
	open, done := &Status{ID: "open"}, &Status{ID: "done"}

	r, err := NewTransitionRule(open, done)
	require.NoError(t, err)
	assert.False(t, r.AllowsAuthor())
	assert.Empty(t, r.GetAllowedRoles(), "a new transition is usable by nobody until LC-3")

	var ve *ValidationError
	_, err = NewTransitionRule(open, open)
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "toId", "a transition from a status to itself is invalid")

	_, err = NewTransitionRule(nil, done)
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "fromId")
	_, err = NewTransitionRule(open, nil)
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "toId")
}

func TestLifecycle_AddTransitionRuleAndFindRule(t *testing.T) {
	open, done, review := &Status{ID: "open"}, &Status{ID: "done"}, &Status{ID: "review"}
	l := NewLifecycle()

	rule, _ := NewTransitionRule(open, done)
	require.NoError(t, l.AddTransitionRule(rule))

	assert.Same(t, rule, l.FindRule(open, done))
	assert.Same(t, rule, l.FindRule(&Status{ID: "open"}, &Status{ID: "done"}), "statuses are compared by id")
	assert.Nil(t, l.FindRule(done, open), "transitions are directed")
	assert.Nil(t, l.FindRule(open, review))
	assert.Nil(t, l.FindRule(nil, done))
	assert.Nil(t, l.FindRule(&Status{}, &Status{}), "unsaved statuses never match")

	duplicate, _ := NewTransitionRule(open, done)
	assert.ErrorIs(t, l.AddTransitionRule(duplicate), ErrTransitionExists)
	assert.Error(t, l.AddTransitionRule(nil))
	assert.Len(t, l.GetRules(), 1)
}

// ---------------------------------------------------------------- LC-3: roles of a transition

func TestTransitionRule_RolesAreUniqueAndRemovable(t *testing.T) {
	rule, _ := NewTransitionRule(&Status{ID: "a"}, &Status{ID: "b"})
	qa, dev := &Role{ID: "qa"}, &Role{ID: "dev"}

	rule.AddRole(qa)
	rule.AddRole(&Role{ID: "qa"}) // same role again
	rule.AddRole(nil)
	rule.AddRole(dev)
	assert.Equal(t, []*Role{qa, dev}, rule.GetAllowedRoles())

	rule.RemoveRole(&Role{ID: "qa"})
	assert.Equal(t, []*Role{dev}, rule.GetAllowedRoles())

	got := rule.GetAllowedRoles()
	got[0] = &Role{ID: "hacker"}
	assert.Equal(t, "dev", rule.GetAllowedRoles()[0].ID, "GetAllowedRoles returns a copy")

	lc := NewLifecycle()
	lc.SetAllowedRole(qa)
	lc.SetAllowedRole(&Role{ID: "qa"})
	lc.SetAllowedRole(nil)
	assert.Len(t, lc.GetAllowedRoles(), 1)
}

// ---------------------------------------------------------------- LC-4 / LC-5: who may make a transition

func TestTransitionRule_IsAllowedFor(t *testing.T) {
	qa := &Role{ID: "qa"}
	author := &User{ID: "alice", Role: &Role{ID: "member"}}
	tester := &User{ID: "quinn", Role: qa}
	stranger := &User{ID: "carol", Role: &Role{ID: "member"}}
	issue := &Issue{Author: author}

	build := func(allowAuthor bool, roles ...*Role) *TransitionRule {
		r, _ := NewTransitionRule(&Status{ID: "a"}, &Status{ID: "b"})
		r.SetAllowAuthor(allowAuthor)
		for _, role := range roles {
			r.AddRole(role)
		}
		return r
	}

	tests := []struct {
		name string
		rule *TransitionRule
		user *User
		want bool
	}{
		{"role allowed", build(false, qa), tester, true},
		{"role not allowed", build(false, qa), stranger, false},
		{"author, allowAuthor", build(true), author, true},
		{"author, allowAuthor off", build(false), author, false},
		{"stranger, allowAuthor", build(true), stranger, false},
		{"author or role: role holder who is not the author", build(true, qa), tester, true},
		{"nobody: no roles, no author", build(false), tester, false},
		{"user without a role", build(false, qa), &User{ID: "x"}, false},
		{"no user", build(true, qa), nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { assert.Equal(t, tc.want, tc.rule.IsAllowedFor(tc.user, issue)) })
	}
	assert.False(t, build(true).IsAllowedFor(author, nil), "without an issue there is no author to match")

	// two users without an id (both unsaved) are not "the same user"
	unsaved := &User{Role: &Role{ID: "member"}}
	assert.False(t, build(true).IsAllowedFor(unsaved, &Issue{Author: &User{}}), "an empty id never matches")
}

func TestLifecycle_CheckTransition(t *testing.T) {
	open, review, done := &Status{ID: "open"}, &Status{ID: "review"}, &Status{ID: "done"}
	qa := &Role{ID: "qa"}
	author := &User{ID: "alice", Role: &Role{ID: "member"}}
	tester := &User{ID: "quinn", Role: qa}
	stranger := &User{ID: "carol", Role: &Role{ID: "member"}}
	issue := &Issue{Author: author, Status: open}

	l := NewLifecycle()
	toReview, _ := NewTransitionRule(open, review)
	toReview.SetAllowAuthor(true)
	toDone, _ := NewTransitionRule(open, done)
	toDone.AddRole(qa)
	locked, _ := NewTransitionRule(review, done) // exists, but nobody may use it
	for _, r := range []*TransitionRule{toReview, toDone, locked} {
		require.NoError(t, l.AddTransitionRule(r))
	}

	tests := []struct {
		name    string
		user    *User
		target  *Status
		allowed bool
		err     error
	}{
		{"author may use an author transition", author, review, true, nil},
		{"role holder may use a role transition", tester, done, true, nil},
		{"stranger may not use an author transition", stranger, review, false, ErrForbidden},
		{"author may not use a role transition", author, done, false, ErrForbidden},
		{"target without a transition", author, &Status{ID: "archived"}, false, ErrTransitionNotAllowed},
		{"staying in the same status is not a transition", author, open, false, ErrTransitionNotAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := l.CheckTransition(issue, tc.user, tc.target)
			assert.Equal(t, tc.allowed, ok)
			assert.ErrorIs(t, err, tc.err)
		})
	}

	// review -> done exists but has no roles and no author flag
	inReview := &Issue{Author: author, Status: review}
	ok, err := l.CheckTransition(inReview, tester, done)
	assert.False(t, ok)
	assert.ErrorIs(t, err, ErrForbidden)

	// the direction matters: done -> open was never defined
	ok, err = l.CheckTransition(&Issue{Author: author, Status: done}, tester, open)
	assert.False(t, ok)
	assert.ErrorIs(t, err, ErrTransitionNotAllowed)

	_, err = l.CheckTransition(&Issue{Author: author}, tester, done)
	assert.Error(t, err, "an issue without a status cannot be moved")
}

func TestIssue_ChangeStatus(t *testing.T) {
	issue := &Issue{Status: &Status{ID: "open"}}
	done := &Status{ID: "done", Name: "Done"}

	require.NoError(t, issue.ChangeStatus(done))
	assert.Same(t, done, issue.GetStatus())

	var ve *ValidationError
	require.ErrorAs(t, issue.ChangeStatus(nil), &ve)
	assert.Contains(t, ve.Fields, "statusId")
	require.ErrorAs(t, issue.ChangeStatus(&Status{}), &ve, "an unsaved status cannot be assigned")
	assert.Same(t, done, issue.GetStatus(), "the status is unchanged after a rejected change")
}

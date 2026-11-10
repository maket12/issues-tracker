package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func mustDate(s string) Date {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return Date{Time: t}
}

func TestUser_HasPermissionDelegatesToRole(t *testing.T) {
	withRole := &User{ID: "alice", Role: &Role{Permissions: []Permission{CreateIssue}}}
	assert.True(t, withRole.HasPermission(CreateIssue))
	assert.False(t, withRole.HasPermission(DeleteIssue))

	assert.False(t, (&User{ID: "no-role"}).HasPermission(CreateIssue), "a user without a role has no permissions")
	assert.False(t, (*User)(nil).HasPermission(CreateIssue))
}

func TestIssue_IsAccessibleBy(t *testing.T) {
	alice, bob, carol := &User{ID: "alice"}, &User{ID: "bob"}, &User{ID: "carol"}
	tests := []struct {
		name  string
		issue Issue
		user  *User
		want  bool
	}{
		{"author", Issue{Author: alice}, alice, true},
		{"assignee", Issue{Author: alice, Assignee: bob}, bob, true},
		{"stranger", Issue{Author: alice, Assignee: bob}, carol, false},
		{"stranger, no assignee", Issue{Author: alice}, carol, false},
		{"nil user", Issue{Author: alice}, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { assert.Equal(t, tc.want, tc.issue.IsAccessibleBy(tc.user)) })
	}
}

func TestNewIssue_StartsTodayWithoutPriorityOrAssignee(t *testing.T) {
	now := time.Date(2026, 11, 10, 1, 30, 0, 0, time.FixedZone("UTC+3", 3*3600))
	author, status := &User{ID: "alice"}, &Status{ID: "open"}

	issue, err := NewIssue("  Fix login ", &IssueType{ID: "bug"}, ptr("  "), author, status, now)
	require.NoError(t, err)

	assert.Equal(t, "Fix login", issue.Name)
	assert.Nil(t, issue.Description, "blank description is stored as absent")
	assert.Same(t, author, issue.Author)
	assert.Same(t, status, issue.Status)
	assert.Nil(t, issue.Priority, "priority is chosen later, in IM-2")
	assert.Nil(t, issue.Assignee)
	assert.Equal(t, "2026-11-09", issue.StartDate.Format("2006-01-02"))
	assert.Empty(t, issue.ID, "the id is assigned when the issue is saved")
}

func TestNewIssue_NameBoundaries(t *testing.T) {
	create := func(name string) error {
		_, err := NewIssue(name, &IssueType{}, nil, &User{}, &Status{}, time.Now())
		return err
	}
	assert.NoError(t, create(strings.Repeat("я", 200)), "200 characters (400 bytes) are allowed")

	var ve *ValidationError
	for _, bad := range []string{"", "   ", strings.Repeat("я", 201)} {
		require.ErrorAs(t, create(bad), &ve, "name %q", bad)
		assert.Contains(t, ve.Fields, "name")
	}
}

func TestIssue_EditValidatesBeforeChangingAnything(t *testing.T) {
	issue := &Issue{Name: "Original", StartDate: mustDate("2026-11-01")}
	typ, prio := &IssueType{ID: "task"}, &Priority{ID: "high"}

	err := issue.Edit("Changed", typ, nil, nil, prio, mustDate("2026-11-10"), ptr(mustDate("2026-11-09")))
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "dueDate")
	assert.Equal(t, "Original", issue.Name)

	require.NoError(t, issue.Edit("Changed", typ, nil, nil, prio, mustDate("2026-11-10"), ptr(mustDate("2026-11-10"))))
	assert.Equal(t, "Changed", issue.Name)
	assert.Same(t, prio, issue.Priority)
}

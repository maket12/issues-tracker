package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

type fakeRepo struct {
	types      map[string]*domain.IssueType
	priorities map[string]*domain.Priority
	users      map[string]*domain.User
	issues     map[string]*domain.Issue
	initial    *domain.Status
	saveErr    error
	saved      *domain.Issue
	saves      int
	lookups    int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		types:      map[string]*domain.IssueType{"bug": {ID: "bug", Name: "Bug"}},
		priorities: map[string]*domain.Priority{"high": {ID: "high", Name: "High"}},
		users:      map[string]*domain.User{"bob": {ID: "bob", Name: "Bob"}},
		issues:     map[string]*domain.Issue{},
		initial:    &domain.Status{ID: "open", Name: "Open"},
	}
}

func (f *fakeRepo) FindByID(_ context.Context, id string) (*domain.Issue, error) {
	i, ok := f.issues[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	c := *i
	return &c, nil
}

func (f *fakeRepo) FindTypeByID(_ context.Context, id string) (*domain.IssueType, error) {
	f.lookups++
	if v, ok := f.types[id]; ok {
		return v, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeRepo) FindPriorityByID(_ context.Context, id string) (*domain.Priority, error) {
	f.lookups++
	if v, ok := f.priorities[id]; ok {
		return v, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeRepo) FindUserByID(_ context.Context, id string) (*domain.User, error) {
	f.lookups++
	if v, ok := f.users[id]; ok {
		return v, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeRepo) FindInitialStatus(context.Context) (*domain.Status, error) {
	if f.initial == nil {
		return nil, domain.ErrNotFound
	}
	return f.initial, nil
}

func (f *fakeRepo) Save(_ context.Context, issue *domain.Issue) (*domain.Issue, error) {
	f.saves++
	f.saved = issue
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	out := *issue
	if out.ID == "" {
		out.ID = "ISS-1"
	}
	return &out, nil
}

func ptr[T any](v T) *T { return &v }

func date(s string) *domain.Date {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &domain.Date{Time: t}
}

func userWith(id string, perms ...domain.Permission) *domain.User {
	return &domain.User{ID: id, Name: id, Role: &domain.Role{ID: "r", Permissions: perms}}
}
func member() *domain.User { return userWith("alice", domain.CreateIssue) }
func viewer() *domain.User { return userWith("vera", domain.ViewIssue) }

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var ve *domain.ValidationError
	require.ErrorAs(t, err, &ve)
	return ve.Fields
}

// ---------------------------------------------------------------- IM-1 createIssue

func TestCreateIssue_PermissionIsCheckedFirst(t *testing.T) {
	repo := newFakeRepo()
	svc := NewIssueService(repo)

	_, err := svc.CreateIssue(context.Background(), "x", "bug", nil, nil)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)

	// even with obviously invalid input, a user without CREATE_ISSUE only learns "forbidden"
	_, err = svc.CreateIssue(context.Background(), "", "no-such-type", nil, viewer())
	assert.ErrorIs(t, err, domain.ErrForbidden)

	assert.Zero(t, repo.lookups, "nothing is looked up for a forbidden caller")
	assert.Zero(t, repo.saves)
}

func TestCreateIssue_FollowsTheSequenceDiagram(t *testing.T) {
	repo := newFakeRepo()
	svc := NewIssueService(repo)
	// 01:30 on 10 Nov in UTC+3 is still 9 Nov in UTC
	svc.now = func() time.Time { return time.Date(2026, 11, 10, 1, 30, 0, 0, time.FixedZone("UTC+3", 3*3600)) }
	author := member()

	saved, err := svc.CreateIssue(context.Background(), "  Fix login  ", " bug ", ptr("  \n "), author)
	require.NoError(t, err)

	assert.Equal(t, "ISS-1", saved.ID)
	issue := repo.saved
	assert.Equal(t, "Fix login", issue.Name)
	assert.Equal(t, "bug", issue.Type.ID, "type found by typeId")
	assert.Equal(t, "open", issue.Status.ID, "status is the initial status, not chosen by the client")
	assert.Same(t, author, issue.Author)
	assert.Nil(t, issue.Description, "blank description is not stored")
	assert.Nil(t, issue.Priority)
	assert.Nil(t, issue.Assignee)
	assert.Equal(t, "2026-11-09", issue.StartDate.Format("2006-01-02"))
}

func TestCreateIssue_RejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name, issueName, typeID string
		description             *string
		field                   string
	}{
		{"blank name", "   ", "bug", nil, "name"},
		{"name too long", strings.Repeat("a", 201), "bug", nil, "name"},
		{"missing type", "Fix", "  ", nil, "typeId"},
		{"unknown type", "Fix", "epic", nil, "typeId"},
		{"description too large", "Fix", "bug", ptr(strings.Repeat("x", domain.MaxDescriptionBytes+1)), "description"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			_, err := NewIssueService(repo).CreateIssue(context.Background(), tc.issueName, tc.typeID, tc.description, member())
			assert.Contains(t, fieldsOf(t, err), tc.field)
			assert.Zero(t, repo.saves, "an invalid issue is never saved")
		})
	}
}

func TestCreateIssue_NoInitialStatusConfigured(t *testing.T) {
	repo := newFakeRepo()
	repo.initial = nil
	_, err := NewIssueService(repo).CreateIssue(context.Background(), "Fix", "bug", nil, member())
	assert.ErrorIs(t, err, domain.ErrNoInitialStatus)
	assert.Zero(t, repo.saves)
}

func TestCreateIssue_RepositoryFailureIsReturned(t *testing.T) {
	boom := errors.New("connection refused")
	repo := newFakeRepo()
	repo.saveErr = boom
	_, err := NewIssueService(repo).CreateIssue(context.Background(), "Fix", "bug", nil, member())
	assert.ErrorIs(t, err, boom)
}

func existingIssue() *domain.Issue {
	return &domain.Issue{
		ID: "ISS-1", Name: "Original", Type: &domain.IssueType{ID: "bug", Name: "Bug"},
		Author: &domain.User{ID: "alice"}, Status: &domain.Status{ID: "open", Name: "Open"},
		Priority: &domain.Priority{ID: "low", Name: "Low"}, StartDate: *date("2026-11-01"), Version: "v1",
	}
}

func validAttrs() AttributeData {
	return AttributeData{
		Name: "New name", TypeID: "bug", Description: ptr("<p>text</p>"), AssigneeID: ptr("bob"),
		PriorityID: "high", StartDate: date("2026-11-10"), DueDate: date("2026-11-20"),
	}
}

func TestFillAttributes_AccessRules(t *testing.T) {
	repo := newFakeRepo()
	issue := existingIssue()
	issue.Assignee = &domain.User{ID: "bob"}
	repo.issues["ISS-1"] = issue
	svc := NewIssueService(repo)
	ctx := context.Background()

	_, err := svc.FillAttributes(ctx, "ISS-1", validAttrs(), nil)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)

	_, err = svc.FillAttributes(ctx, "ISS-404", validAttrs(), member())
	assert.ErrorIs(t, err, domain.ErrNotFound)

	_, err = svc.FillAttributes(ctx, "ISS-1", validAttrs(), userWith("carol", domain.CreateIssue))
	assert.ErrorIs(t, err, domain.ErrForbidden, "neither author nor assignee")
	assert.Zero(t, repo.lookups, "access is checked before any attribute is looked up")
	assert.Zero(t, repo.saves)

	_, err = svc.FillAttributes(ctx, "ISS-1", validAttrs(), userWith("alice")) // author, no special permission needed
	assert.NoError(t, err)
	_, err = svc.FillAttributes(ctx, "ISS-1", validAttrs(), userWith("bob")) // assignee
	assert.NoError(t, err)
}

func TestFillAttributes_ChangesAttributesButNotAuthorOrStatus(t *testing.T) {
	repo := newFakeRepo()
	repo.issues["ISS-1"] = existingIssue()

	updated, err := NewIssueService(repo).FillAttributes(context.Background(), "ISS-1", validAttrs(), member())
	require.NoError(t, err)

	assert.Equal(t, "ISS-1", updated.ID)
	assert.Equal(t, "New name", updated.Name)
	assert.Equal(t, "high", updated.Priority.ID)
	assert.Equal(t, "bob", updated.Assignee.ID)
	assert.Equal(t, "2026-11-20", updated.DueDate.Format("2006-01-02"))
	assert.Equal(t, "alice", updated.Author.ID)
	assert.Equal(t, "open", updated.Status.ID)
}

func TestFillAttributes_ReportsEveryProblemAtOnce(t *testing.T) {
	repo := newFakeRepo()
	repo.issues["ISS-1"] = existingIssue()
	attrs := validAttrs()
	attrs.TypeID, attrs.PriorityID, attrs.AssigneeID = "epic", "urgent", ptr("nobody")

	_, err := NewIssueService(repo).FillAttributes(context.Background(), "ISS-1", attrs, member())
	fields := fieldsOf(t, err)
	for _, f := range []string{"typeId", "priorityId", "assigneeId"} {
		assert.Contains(t, fields, f)
	}
	assert.Zero(t, repo.saves)
}

func TestFillAttributes_RequiredAndOptionalAttributes(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*AttributeData)
		field   string
	}{
		{"missing type", func(a *AttributeData) { a.TypeID = "" }, "typeId"},
		{"missing priority", func(a *AttributeData) { a.PriorityID = " " }, "priorityId"},
		{"missing start date", func(a *AttributeData) { a.StartDate = nil }, "startDate"},
		{"blank name", func(a *AttributeData) { a.Name = "  " }, "name"},
		{"due before start", func(a *AttributeData) { a.DueDate = date("2026-11-09") }, "dueDate"},
		{"due equals start", func(a *AttributeData) { a.DueDate = date("2026-11-10") }, ""},
		{"no due date", func(a *AttributeData) { a.DueDate = nil }, ""},
		{"no assignee", func(a *AttributeData) { a.AssigneeID = nil }, ""},
		{"blank assignee", func(a *AttributeData) { a.AssigneeID = ptr("  ") }, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			repo.issues["ISS-1"] = existingIssue()
			attrs := validAttrs()
			tc.mutate(&attrs)

			_, err := NewIssueService(repo).FillAttributes(context.Background(), "ISS-1", attrs, member())
			if tc.field == "" {
				require.NoError(t, err)
				if attrs.AssigneeID == nil || strings.TrimSpace(*attrs.AssigneeID) == "" {
					assert.Nil(t, repo.saved.Assignee, "no assignee means unassigned")
				}
				return
			}
			assert.Contains(t, fieldsOf(t, err), tc.field)
			assert.Zero(t, repo.saves)
		})
	}
}

func TestFillAttributes_ConcurrentModificationIsReported(t *testing.T) {
	repo := newFakeRepo()
	repo.issues["ISS-1"] = existingIssue()
	repo.saveErr = domain.ErrConflict

	_, err := NewIssueService(repo).FillAttributes(context.Background(), "ISS-1", validAttrs(), member())
	assert.ErrorIs(t, err, domain.ErrConflict)
}

// Tests for MA-4 (check permissions), MA-5 (work simultaneously) and LC-1 .. LC-4 (statuses, transitions,
// transition rules, change issue status), with mocked repositories. The mocks write every call into a
// shared log, so the tests can check the call order of the sequence diagrams.
package service_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maket12/issues-tracker/backend/internal/domain"
	"github.com/maket12/issues-tracker/backend/internal/service"
)

// ================================================================ mocks

var (
	_ service.IssueRepository     = (*lcIssueRepo)(nil)
	_ service.StatusRepository    = (*lcStatusRepo)(nil)
	_ service.LifecycleRepository = (*lcLifecycleRepo)(nil)
)

// lcIssueRepo behaves like the real repository where it matters here: FindByID returns a copy, and Save
// succeeds only if the issue still has the stored version (then the version is increased).
type lcIssueRepo struct {
	log        *callLog
	stored     map[string]*domain.Issue
	statuses   map[string]*domain.Status
	beforeSave func() // lets a test play "somebody else saved in between"
	saves      int
}

func newLCIssueRepo(log *callLog, issues ...*domain.Issue) *lcIssueRepo {
	m := &lcIssueRepo{log: log, stored: map[string]*domain.Issue{}, statuses: map[string]*domain.Status{}}
	for _, i := range issues {
		c := *i
		m.stored[i.ID] = &c
	}
	return m
}

func (m *lcIssueRepo) FindByID(_ context.Context, id string) (*domain.Issue, error) {
	m.log.add("IssueRepository.FindByID(%s)", id)
	i, ok := m.stored[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	c := *i
	return &c, nil
}

func (m *lcIssueRepo) FindStatusByID(_ context.Context, id string) (*domain.Status, error) {
	m.log.add("IssueRepository.FindStatusByID(%s)", id)
	if s, ok := m.statuses[id]; ok {
		return s, nil
	}
	return nil, domain.ErrNotFound
}

func (m *lcIssueRepo) Save(_ context.Context, issue *domain.Issue) (*domain.Issue, error) {
	m.log.add("IssueRepository.Save(%s)", issue.ID)
	m.saves++
	if m.beforeSave != nil {
		m.beforeSave()
	}
	cur, ok := m.stored[issue.ID]
	if !ok || cur.Version != issue.Version {
		return nil, domain.ErrConflict
	}
	n, _ := strconv.Atoi(issue.Version)
	out := *issue
	out.Version = strconv.Itoa(n + 1)
	m.stored[out.ID] = &out
	c := out
	return &c, nil
}

// the attribute lookups of editIssue are not what these tests are about: they are logged with a prefix
func (m *lcIssueRepo) FindTypeByID(_ context.Context, id string) (*domain.IssueType, error) {
	m.log.add("lookup:type(%s)", id)
	if id == "bug" {
		return &domain.IssueType{ID: "bug", Name: "Bug"}, nil
	}
	return nil, domain.ErrNotFound
}
func (m *lcIssueRepo) FindPriorityByID(_ context.Context, id string) (*domain.Priority, error) {
	m.log.add("lookup:priority(%s)", id)
	if id == "high" {
		return &domain.Priority{ID: "high", Name: "High"}, nil
	}
	return nil, domain.ErrNotFound
}
func (m *lcIssueRepo) FindUserByID(_ context.Context, id string) (*domain.User, error) {
	m.log.add("lookup:user(%s)", id)
	if id == "bob" {
		return &domain.User{ID: "bob", Name: "Bob"}, nil
	}
	return nil, domain.ErrNotFound
}
func (m *lcIssueRepo) FindInitialStatus(context.Context) (*domain.Status, error) {
	m.log.add("UNEXPECTED IssueRepository.FindInitialStatus")
	return nil, errors.New("unexpected call")
}
func (m *lcIssueRepo) Delete(context.Context, *domain.Issue) (bool, error) {
	m.log.add("UNEXPECTED IssueRepository.Delete")
	return false, errors.New("unexpected call")
}

func withoutLookups(entries []string) []string {
	var out []string
	for _, e := range entries {
		if !strings.HasPrefix(e, "lookup:") {
			out = append(out, e)
		}
	}
	return out
}

type lcStatusRepo struct {
	log     *callLog
	byID    map[string]*domain.Status
	findErr error
	saveErr error
	nextID  int
}

func (m *lcStatusRepo) FindByID(_ context.Context, id string) (*domain.Status, error) {
	m.log.add("StatusRepository.FindByID(%s)", id)
	if m.findErr != nil {
		return nil, m.findErr
	}
	if s, ok := m.byID[id]; ok {
		return s, nil
	}
	return nil, domain.ErrNotFound
}

// Save follows the documented contract: a new status gets its id assigned in place.
func (m *lcStatusRepo) Save(_ context.Context, s *domain.Status) (*domain.Status, error) {
	m.log.add("StatusRepository.Save(%s)", s.Name)
	if m.saveErr != nil {
		return nil, m.saveErr
	}
	if s.ID == "" {
		m.nextID++
		s.ID = "STS-" + strconv.Itoa(m.nextID)
	}
	return s, nil
}

type lcLifecycleRepo struct {
	log       *callLog
	lifecycle *domain.Lifecycle
	getErr    error
	saveErr   error
	saved     *domain.Lifecycle
}

func (m *lcLifecycleRepo) GetLifecycle(context.Context) (*domain.Lifecycle, error) {
	m.log.add("LifecycleRepository.GetLifecycle")
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.lifecycle, nil
}

func (m *lcLifecycleRepo) Save(_ context.Context, l *domain.Lifecycle) (*domain.Lifecycle, error) {
	m.log.add("LifecycleRepository.Save")
	if m.saveErr != nil {
		return nil, m.saveErr
	}
	m.saved = l
	return l, nil
}

// ================================================================ MA-4 checkPermissions

func TestCheckPermissions_FollowsTheSequenceDiagram(t *testing.T) {
	log := &callLog{}
	user := userWith("USR-1", domain.CreateIssue, domain.ViewIssue)
	users := &ma23UserRepo{log: log, byID: map[string]*domain.User{"USR-1": user}}
	svc := service.NewAccessService(users)

	granted, err := svc.CheckPermissions(ctx, "USR-1", domain.ViewIssue)
	require.NoError(t, err)
	assert.True(t, granted)
	assert.Equal(t, []string{"UserRepository.FindByID(USR-1)"}, log.entries)

	granted, err = svc.CheckPermissions(ctx, "USR-1", domain.DeleteIssue)
	assert.False(t, granted)
	assert.ErrorIs(t, err, domain.ErrForbidden, "access denied")
}

func TestCheckPermissions_TheRoleIsDeterminedOnEveryCall(t *testing.T) {
	log := &callLog{}
	user := userWith("USR-1", domain.ViewIssue)
	users := &ma23UserRepo{log: log, byID: map[string]*domain.User{"USR-1": user}}
	svc := service.NewAccessService(users)

	granted, _ := svc.CheckPermissions(ctx, "USR-1", domain.ManageRoles)
	assert.False(t, granted)

	user.Role = &domain.Role{ID: "admin", Permissions: []domain.Permission{domain.ManageRoles}} // role changed (MA-3)
	granted, err := svc.CheckPermissions(ctx, "USR-1", domain.ManageRoles)
	require.NoError(t, err)
	assert.True(t, granted, "the new role is effective at once")
}

func TestCheckPermissions_EdgeCases(t *testing.T) {
	boom := errors.New("db down")
	log := &callLog{}
	users := &ma23UserRepo{log: log, byID: map[string]*domain.User{
		"USR-1": userWith("USR-1"), // a role that grants nothing
		"USR-2": {ID: "USR-2"},     // no role at all
	}}
	svc := service.NewAccessService(users)

	for _, id := range []string{"USR-1", "USR-2"} {
		granted, err := svc.CheckPermissions(ctx, id, domain.ViewIssue)
		assert.False(t, granted, id)
		assert.ErrorIs(t, err, domain.ErrForbidden, id)
	}

	granted, err := svc.CheckPermissions(ctx, "USR-404", domain.ViewIssue)
	assert.False(t, granted)
	assert.ErrorIs(t, err, domain.ErrUnauthorized, "an unknown user is not an authenticated user")

	users.findErr = boom
	granted, err = svc.CheckPermissions(ctx, "USR-1", domain.ViewIssue)
	assert.False(t, granted)
	assert.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, domain.ErrForbidden, "a failure is not the same as a denial")
}

// ================================================================ MA-5 editIssue with expectedVersion

type editFixture struct {
	log    *callLog
	issues *lcIssueRepo
	svc    *service.IssueService
}

func newEditFixture() *editFixture {
	log := &callLog{}
	issue := &domain.Issue{
		ID: "ISS-1", Name: "Original", Type: &domain.IssueType{ID: "bug", Name: "Bug"},
		Author: &domain.User{ID: "alice"}, Assignee: &domain.User{ID: "bob"},
		Status: &domain.Status{ID: "open", Name: "Open"}, Priority: &domain.Priority{ID: "high", Name: "High"},
		StartDate: day("2026-11-01"), Version: "1",
	}
	issues := newLCIssueRepo(log, issue)
	return &editFixture{log: log, issues: issues, svc: service.NewIssueService(issues, nil, nil)}
}

// changesNamed keeps bob as the assignee: an edit replaces all attributes, so leaving the assignee out
// would unassign him (and he would lose access to the issue).
func changesNamed(name string) service.AttributeData {
	start, assignee := day("2026-11-10"), "bob"
	return service.AttributeData{Name: name, TypeID: "bug", PriorityID: "high", AssigneeID: &assignee, StartDate: &start}
}

func TestEditIssue_TwoUsersEditTheSameIssue(t *testing.T) {
	f := newEditFixture()
	userA := userWith("alice", domain.EditIssue) // the author
	userB := userWith("bob", domain.EditIssue)   // the assignee; both opened the issue at version 1

	updated, err := f.svc.EditIssue(ctx, "ISS-1", changesNamed("Edited by A"), "1", userA)
	require.NoError(t, err)
	assert.Equal(t, "Edited by A", updated.Name)
	assert.Equal(t, "2", updated.Version, "the save produced version 2")

	updated, err = f.svc.EditIssue(ctx, "ISS-1", changesNamed("Edited by B"), "1", userB)
	assert.ErrorIs(t, err, domain.ErrConflict, "B is asked to refresh")
	assert.Nil(t, updated)

	assert.Equal(t, []string{
		"IssueRepository.FindByID(ISS-1)", "IssueRepository.Save(ISS-1)", // A
		"IssueRepository.FindByID(ISS-1)", // B: conflict detected, nothing is saved
	}, withoutLookups(f.log.entries))
	assert.Equal(t, "Edited by A", f.issues.stored["ISS-1"].Name, "the later edit never overwrites the earlier one")

	// after refreshing (version 2) B can save
	_, err = f.svc.EditIssue(ctx, "ISS-1", changesNamed("Edited by B"), "2", userB)
	require.NoError(t, err)
	assert.Equal(t, "3", f.issues.stored["ISS-1"].Version)
}

func TestEditIssue_ExpectedVersionIsRequired(t *testing.T) {
	f := newEditFixture()
	for _, v := range []string{"", "   "} {
		_, err := f.svc.EditIssue(ctx, "ISS-1", changesNamed("X"), v, userWith("alice", domain.EditIssue))
		assert.Contains(t, fieldsOf(t, err), "version", "expectedVersion %q", v)
	}
	assert.Zero(t, f.issues.saves)
}

func TestEditIssue_AccessIsCheckedBeforeTheVersion(t *testing.T) {
	f := newEditFixture()
	editor := func(id string) *domain.User { return userWith(id, domain.EditIssue) }

	_, err := f.svc.EditIssue(ctx, "ISS-1", changesNamed("X"), "1", nil)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)

	_, err = f.svc.EditIssue(ctx, "ISS-1", changesNamed("X"), "1", userWith("alice", domain.ViewIssue))
	assert.ErrorIs(t, err, domain.ErrForbidden, "EDIT_ISSUE is required")
	assert.Equal(t, []string(nil), f.log.entries, "nothing is read for a user without the permission")

	_, err = f.svc.EditIssue(ctx, "ISS-404", changesNamed("X"), "1", editor("alice"))
	assert.ErrorIs(t, err, domain.ErrNotFound)

	// a stranger gets "access denied", not "conflict": the version of an issue is not revealed to them
	_, err = f.svc.EditIssue(ctx, "ISS-1", changesNamed("X"), "999", editor("carol"))
	assert.ErrorIs(t, err, domain.ErrForbidden)
	assert.Zero(t, f.issues.saves)
}

func TestEditIssue_ASaveBetweenLoadAndSaveIsDetected(t *testing.T) {
	f := newEditFixture()
	// the version matches when the issue is loaded, but somebody else saves before this edit does
	f.issues.beforeSave = func() { f.issues.stored["ISS-1"].Version = "2" }

	_, err := f.svc.EditIssue(ctx, "ISS-1", changesNamed("Late"), "1", userWith("alice", domain.EditIssue))

	assert.ErrorIs(t, err, domain.ErrConflict)
	assert.Equal(t, "Original", f.issues.stored["ISS-1"].Name)
}

// ================================================================ LC-4 changeIssueStatus (with LC-5)

type statusFixture struct {
	log        *callLog
	issues     *lcIssueRepo
	lifecycles *lcLifecycleRepo
	svc        *service.IssueService
}

var (
	author   = &domain.User{ID: "alice", Role: &domain.Role{ID: "member"}}
	tester   = &domain.User{ID: "quinn", Role: &domain.Role{ID: "qa"}}
	stranger = &domain.User{ID: "carol", Role: &domain.Role{ID: "member"}}
)

// open -> done: role qa;  open -> review: the author;  review -> done: exists, but nobody may use it
func newStatusFixture() *statusFixture {
	log := &callLog{}
	open, review, done := &domain.Status{ID: "open", Name: "Open"}, &domain.Status{ID: "review", Name: "Review"}, &domain.Status{ID: "done", Name: "Done"}

	toDone, _ := domain.NewTransitionRule(open, done)
	toDone.AddRole(&domain.Role{ID: "qa"})
	toReview, _ := domain.NewTransitionRule(open, review)
	toReview.SetAllowAuthor(true)
	locked, _ := domain.NewTransitionRule(review, done)
	lifecycle := domain.RestoreLifecycle([]*domain.Status{open, review, done}, []*domain.TransitionRule{toDone, toReview, locked})

	issue := &domain.Issue{
		ID: "ISS-1", Name: "Original", Type: &domain.IssueType{ID: "bug"}, Author: &domain.User{ID: "alice"},
		Status: open, Priority: &domain.Priority{ID: "high"}, StartDate: day("2026-11-01"), Version: "1",
	}
	issues := newLCIssueRepo(log, issue)
	issues.statuses = map[string]*domain.Status{
		"open": open, "review": review, "done": done, "archived": {ID: "archived", Name: "Archived"}, // archived: no transitions at all
	}
	lifecycles := &lcLifecycleRepo{log: log, lifecycle: lifecycle}
	return &statusFixture{log: log, issues: issues, lifecycles: lifecycles, svc: service.NewIssueService(issues, nil, lifecycles)}
}

func TestChangeIssueStatus_FollowsTheSequenceDiagram(t *testing.T) {
	f := newStatusFixture()

	updated, err := f.svc.ChangeIssueStatus(ctx, "ISS-1", "done", tester)

	require.NoError(t, err)
	assert.Equal(t, "done", updated.Status.ID)
	assert.Equal(t, "2", updated.Version, "a status change is a save like any other")
	assert.Equal(t, []string{
		"IssueRepository.FindByID(ISS-1)",
		"IssueRepository.FindStatusByID(done)",
		"LifecycleRepository.GetLifecycle",
		"IssueRepository.Save(ISS-1)",
	}, f.log.entries)
	assert.Equal(t, "done", f.issues.stored["ISS-1"].Status.ID)
}

func TestChangeIssueStatus_TheAuthorMayUseAnAuthorTransition(t *testing.T) {
	f := newStatusFixture()
	updated, err := f.svc.ChangeIssueStatus(ctx, "ISS-1", "review", author)
	require.NoError(t, err)
	assert.Equal(t, "review", updated.Status.ID)
}

func TestChangeIssueStatus_Rejections(t *testing.T) {
	getLifecycle := []string{"IssueRepository.FindByID(ISS-1)", "IssueRepository.FindStatusByID(%s)", "LifecycleRepository.GetLifecycle"}
	tests := []struct {
		name    string
		user    *domain.User
		target  string
		inState string // status the issue is in, "" = open
		err     error
	}{
		{"role lacking rights", stranger, "done", "", domain.ErrForbidden},
		{"the author may not use a role transition", author, "done", "", domain.ErrForbidden},
		{"a role holder who is not the author may not use an author transition", tester, "review", "", domain.ErrForbidden},
		{"a transition nobody may use", tester, "done", "review", domain.ErrForbidden},
		{"target status has no transition from here", tester, "archived", "", domain.ErrTransitionNotAllowed},
		{"staying in the same status is not a transition", author, "open", "", domain.ErrTransitionNotAllowed},
		{"the direction matters", tester, "open", "done", domain.ErrTransitionNotAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newStatusFixture()
			if tc.inState != "" {
				f.issues.stored["ISS-1"].Status = f.issues.statuses[tc.inState]
			}

			updated, err := f.svc.ChangeIssueStatus(ctx, "ISS-1", tc.target, tc.user)

			assert.ErrorIs(t, err, tc.err)
			assert.Nil(t, updated)
			assert.Zero(t, f.issues.saves, "a rejected change is never saved")
			wantLog := []string{getLifecycle[0], strings.Replace(getLifecycle[1], "%s", tc.target, 1), getLifecycle[2]}
			assert.Equal(t, wantLog, f.log.entries)
		})
	}
}

func TestChangeIssueStatus_InputProblems(t *testing.T) {
	t.Run("not logged in", func(t *testing.T) {
		f := newStatusFixture()
		_, err := f.svc.ChangeIssueStatus(ctx, "ISS-1", "done", nil)
		assert.ErrorIs(t, err, domain.ErrUnauthorized)
		assert.Empty(t, f.log.entries)
	})
	t.Run("unknown issue", func(t *testing.T) {
		f := newStatusFixture()
		_, err := f.svc.ChangeIssueStatus(ctx, "ISS-404", "done", tester)
		assert.ErrorIs(t, err, domain.ErrNotFound)
		assert.Equal(t, []string{"IssueRepository.FindByID(ISS-404)"}, f.log.entries)
	})
	t.Run("unknown target status", func(t *testing.T) {
		f := newStatusFixture()
		_, err := f.svc.ChangeIssueStatus(ctx, "ISS-1", "limbo", tester)
		assert.Contains(t, fieldsOf(t, err), "statusId")
		assert.NotContains(t, f.log.entries, "LifecycleRepository.GetLifecycle", "no lifecycle is read for an unknown status")
	})
	t.Run("blank target status", func(t *testing.T) {
		f := newStatusFixture()
		_, err := f.svc.ChangeIssueStatus(ctx, "ISS-1", "  ", tester)
		assert.Contains(t, fieldsOf(t, err), "statusId")
	})
}

func TestChangeIssueStatus_ConcurrentChangeIsDetectedOnSave(t *testing.T) {
	f := newStatusFixture()
	// somebody else edits the issue after it was loaded here
	f.issues.beforeSave = func() { f.issues.stored["ISS-1"].Version = "2" }

	_, err := f.svc.ChangeIssueStatus(ctx, "ISS-1", "done", tester)

	assert.ErrorIs(t, err, domain.ErrConflict)
	assert.Equal(t, "open", f.issues.stored["ISS-1"].Status.ID, "the issue keeps its status")
}

func TestChangeIssueStatus_LifecycleFailureStopsTheFlow(t *testing.T) {
	boom := errors.New("db down")
	f := newStatusFixture()
	f.lifecycles.getErr = boom

	_, err := f.svc.ChangeIssueStatus(ctx, "ISS-1", "done", tester)

	assert.ErrorIs(t, err, boom)
	assert.Zero(t, f.issues.saves)
}

// ================================================================ LC-1 .. LC-3

type lcFixture struct {
	log        *callLog
	statuses   *lcStatusRepo
	lifecycles *lcLifecycleRepo
	roles      *ma23RoleRepo
	svc        *service.LifecycleService
}

func admin() *domain.User { return userWith("dana", domain.ConfigureLifecycle) }

func newLCFixture() *lcFixture {
	log := &callLog{}
	open, review, done := &domain.Status{ID: "open", Name: "Open"}, &domain.Status{ID: "review", Name: "Review"}, &domain.Status{ID: "done", Name: "Done"}
	toDone, _ := domain.NewTransitionRule(open, done)
	statuses := &lcStatusRepo{log: log, byID: map[string]*domain.Status{"open": open, "review": review, "done": done}}
	lifecycles := &lcLifecycleRepo{log: log, lifecycle: domain.RestoreLifecycle(
		[]*domain.Status{open, review, done}, []*domain.TransitionRule{toDone})}
	roles := &ma23RoleRepo{log: log, byID: map[string]*domain.Role{"qa": {ID: "qa", Name: "QA"}, "dev": {ID: "dev", Name: "Dev"}}}
	return &lcFixture{log: log, statuses: statuses, lifecycles: lifecycles, roles: roles,
		svc: service.NewLifecycleService(lifecycles, statuses, roles)}
}

func TestLifecycleRights(t *testing.T) {
	// every LC operation needs CONFIGURE_LIFECYCLE, checked before anything is read
	operations := map[string]func(f *lcFixture, u *domain.User) error{
		"LC-1": func(f *lcFixture, u *domain.User) error { _, err := f.svc.DefineStatus(ctx, "New", u); return err },
		"LC-2": func(f *lcFixture, u *domain.User) error {
			_, err := f.svc.DefineTransition(ctx, "open", "review", u)
			return err
		},
		"LC-3": func(f *lcFixture, u *domain.User) error {
			_, err := f.svc.SetTransitionRulesByID(ctx, "open", "done", "qa", false, u)
			return err
		},
	}
	for name, op := range operations {
		t.Run(name, func(t *testing.T) {
			f := newLCFixture()
			assert.ErrorIs(t, op(f, nil), domain.ErrUnauthorized)
			assert.ErrorIs(t, op(f, userWith("alice", domain.CreateIssue, domain.EditIssue, domain.ManageRoles)), domain.ErrForbidden)
			assert.Empty(t, f.log.entries, "no repository is touched without the right")
		})
	}
}

func TestDefineStatus_FollowsTheSequenceDiagram(t *testing.T) {
	f := newLCFixture()

	saved, err := f.svc.DefineStatus(ctx, "  In Review ", admin())

	require.NoError(t, err)
	assert.Equal(t, "STS-1", saved.ID)
	assert.Equal(t, "In Review", saved.Name)
	assert.Equal(t, []string{
		"LifecycleRepository.GetLifecycle",
		"StatusRepository.Save(In Review)",
		"LifecycleRepository.Save",
	}, f.log.entries)

	statuses := f.lifecycles.saved.GetStatuses()
	require.Len(t, statuses, 4)
	assert.Same(t, saved, statuses[3], "the lifecycle holds the status that was stored, with its id")
}

func TestDefineStatus_RejectsEmptyAndDuplicateNames(t *testing.T) {
	for name, c := range map[string]struct {
		input string
		check func(t *testing.T, err error)
	}{
		"empty":                 {"", func(t *testing.T, err error) { assert.Contains(t, fieldsOf(t, err), "name") }},
		"blank":                 {"   ", func(t *testing.T, err error) { assert.Contains(t, fieldsOf(t, err), "name") }},
		"duplicate":             {"Review", func(t *testing.T, err error) { assert.ErrorIs(t, err, domain.ErrStatusExists) }},
		"duplicate, other case": {"  dONE ", func(t *testing.T, err error) { assert.ErrorIs(t, err, domain.ErrStatusExists) }},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLCFixture()
			_, err := f.svc.DefineStatus(ctx, c.input, admin())
			c.check(t, err)
			assert.Equal(t, []string{"LifecycleRepository.GetLifecycle"}, f.log.entries, "nothing is saved")
		})
	}
}

func TestDefineStatus_NameTakenAfterTheLifecycleWasRead(t *testing.T) {
	f := newLCFixture()
	f.statuses.saveErr = domain.ErrStatusExists // the unique index fires: a concurrent request won

	_, err := f.svc.DefineStatus(ctx, "Fresh", admin())

	assert.ErrorIs(t, err, domain.ErrStatusExists)
	assert.NotContains(t, f.log.entries, "LifecycleRepository.Save")
}

func TestLifecycleOperations_InfrastructureFailuresStopTheFlow(t *testing.T) {
	boom := errors.New("db down")
	get, saveStatus, saveLifecycle := "LifecycleRepository.GetLifecycle", "StatusRepository.Save(Fresh)", "LifecycleRepository.Save"
	tests := []struct {
		name    string
		setup   func(f *lcFixture)
		run     func(f *lcFixture) error
		wantLog []string
	}{
		{"LC-1 lifecycle cannot be read", func(f *lcFixture) { f.lifecycles.getErr = boom },
			func(f *lcFixture) error { _, err := f.svc.DefineStatus(ctx, "Fresh", admin()); return err }, []string{get}},
		{"LC-1 status cannot be saved", func(f *lcFixture) { f.statuses.saveErr = boom },
			func(f *lcFixture) error { _, err := f.svc.DefineStatus(ctx, "Fresh", admin()); return err }, []string{get, saveStatus}},
		{"LC-1 lifecycle cannot be saved", func(f *lcFixture) { f.lifecycles.saveErr = boom },
			func(f *lcFixture) error { _, err := f.svc.DefineStatus(ctx, "Fresh", admin()); return err }, []string{get, saveStatus, saveLifecycle}},
		{"LC-2 status lookup fails", func(f *lcFixture) { f.statuses.findErr = boom },
			func(f *lcFixture) error { _, err := f.svc.DefineTransition(ctx, "open", "review", admin()); return err },
			[]string{"StatusRepository.FindByID(open)"}},
		{"LC-2 lifecycle cannot be saved", func(f *lcFixture) { f.lifecycles.saveErr = boom },
			func(f *lcFixture) error { _, err := f.svc.DefineTransition(ctx, "open", "review", admin()); return err },
			[]string{"StatusRepository.FindByID(open)", "StatusRepository.FindByID(review)", get, saveLifecycle}},
		{"LC-3 role lookup fails", func(f *lcFixture) { f.roles.findErr = boom },
			func(f *lcFixture) error {
				_, err := f.svc.SetTransitionRulesByID(ctx, "open", "done", "qa", false, admin())
				return err
			},
			[]string{"StatusRepository.FindByID(open)", "StatusRepository.FindByID(done)", get, "RoleRepository.FindByID(qa)"}},
		{"LC-3 lifecycle cannot be saved", func(f *lcFixture) { f.lifecycles.saveErr = boom },
			func(f *lcFixture) error {
				_, err := f.svc.SetTransitionRulesByID(ctx, "open", "done", "qa", false, admin())
				return err
			},
			[]string{"StatusRepository.FindByID(open)", "StatusRepository.FindByID(done)", get, "RoleRepository.FindByID(qa)", saveLifecycle}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newLCFixture()
			tc.setup(f)
			err := tc.run(f)
			assert.ErrorIs(t, err, boom)
			assert.Equal(t, tc.wantLog, f.log.entries)
		})
	}
}

func TestDefineTransition_FollowsTheSequenceDiagram(t *testing.T) {
	f := newLCFixture()

	rule, err := f.svc.DefineTransition(ctx, "open", "review", admin())

	require.NoError(t, err)
	assert.Equal(t, []string{
		"StatusRepository.FindByID(open)",
		"StatusRepository.FindByID(review)",
		"LifecycleRepository.GetLifecycle",
		"LifecycleRepository.Save",
	}, f.log.entries)
	assert.True(t, rule.Matches(f.statuses.byID["open"], f.statuses.byID["review"]))
	assert.False(t, rule.AllowsAuthor(), "a new transition is usable by nobody until LC-3")
	assert.Empty(t, rule.GetAllowedRoles())
	assert.NotNil(t, f.lifecycles.saved.FindRule(f.statuses.byID["open"], f.statuses.byID["review"]))
}

func TestDefineTransition_RejectsInvalidLinks(t *testing.T) {
	tests := []struct {
		name          string
		from, to      string
		wantErr       error
		wantFields    []string
		wantLog       []string
		lifecycleRead bool
	}{
		{"unknown source and target are both reported", "x", "y", nil, []string{"fromId", "toId"},
			[]string{"StatusRepository.FindByID(x)", "StatusRepository.FindByID(y)"}, false},
		{"missing ids", "", "  ", nil, []string{"fromId", "toId"}, nil, false},
		{"a status cannot lead to itself", "open", "open", nil, []string{"toId"},
			[]string{"StatusRepository.FindByID(open)", "StatusRepository.FindByID(open)", "LifecycleRepository.GetLifecycle"}, true},
		{"duplicate link", "open", "done", domain.ErrTransitionExists, nil,
			[]string{"StatusRepository.FindByID(open)", "StatusRepository.FindByID(done)", "LifecycleRepository.GetLifecycle"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newLCFixture()
			_, err := f.svc.DefineTransition(ctx, tc.from, tc.to, admin())

			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			}
			for _, field := range tc.wantFields {
				assert.Contains(t, fieldsOf(t, err), field)
			}
			assert.Equal(t, tc.wantLog, f.log.entries)
			assert.Nil(t, f.lifecycles.saved, "an invalid link is never saved")
		})
	}
}

func TestSetTransitionRules_FollowsTheSequenceDiagram(t *testing.T) {
	f := newLCFixture()
	open, done := f.statuses.byID["open"], f.statuses.byID["done"]

	rule, err := f.svc.SetTransitionRules(ctx, open, done, "qa", true, admin())

	require.NoError(t, err)
	assert.Equal(t, []string{
		"LifecycleRepository.GetLifecycle",
		"RoleRepository.FindByID(qa)",
		"LifecycleRepository.Save",
	}, f.log.entries)
	require.Len(t, rule.GetAllowedRoles(), 1)
	assert.Equal(t, "qa", rule.GetAllowedRoles()[0].ID)
	assert.True(t, rule.AllowsAuthor())
	assert.Len(t, f.lifecycles.saved.GetAllowedRoles(), 1, "the role is registered with the lifecycle")
	assert.Same(t, rule, f.lifecycles.saved.FindRule(open, done), "the returned rule is the stored one")
}

func TestSetTransitionRules_RolesAccumulateAndAllowAuthorFollowsTheLastCall(t *testing.T) {
	f := newLCFixture()
	open, done := f.statuses.byID["open"], f.statuses.byID["done"]

	_, err := f.svc.SetTransitionRules(ctx, open, done, "qa", true, admin())
	require.NoError(t, err)
	_, err = f.svc.SetTransitionRules(ctx, open, done, "qa", true, admin()) // same role again
	require.NoError(t, err)
	rule, err := f.svc.SetTransitionRules(ctx, open, done, "dev", false, admin())
	require.NoError(t, err)

	ids := []string{}
	for _, r := range rule.GetAllowedRoles() {
		ids = append(ids, r.ID)
	}
	assert.Equal(t, []string{"qa", "dev"}, ids, "each role once")
	assert.False(t, rule.AllowsAuthor())
}

func TestSetTransitionRules_Rejections(t *testing.T) {
	t.Run("the transition is not defined", func(t *testing.T) {
		f := newLCFixture()
		_, err := f.svc.SetTransitionRules(ctx, f.statuses.byID["done"], f.statuses.byID["open"], "qa", false, admin())
		assert.ErrorIs(t, err, domain.ErrNotFound)
		assert.Equal(t, []string{"LifecycleRepository.GetLifecycle"}, f.log.entries, "no role is looked up")
	})
	t.Run("role missing", func(t *testing.T) {
		f := newLCFixture()
		_, err := f.svc.SetTransitionRules(ctx, f.statuses.byID["open"], f.statuses.byID["done"], "  ", false, admin())
		assert.Contains(t, fieldsOf(t, err), "roleId")
		assert.Nil(t, f.lifecycles.saved)
	})
	t.Run("unknown role: the rule is not saved", func(t *testing.T) {
		f := newLCFixture()
		_, err := f.svc.SetTransitionRules(ctx, f.statuses.byID["open"], f.statuses.byID["done"], "ghost", false, admin())
		assert.Contains(t, fieldsOf(t, err), "roleId")
		assert.Nil(t, f.lifecycles.saved)
		assert.Empty(t, f.lifecycles.lifecycle.FindRule(f.statuses.byID["open"], f.statuses.byID["done"]).GetAllowedRoles())
	})
}

func TestSetTransitionRulesByID_ResolvesTheStatusesFirst(t *testing.T) {
	f := newLCFixture()
	rule, err := f.svc.SetTransitionRulesByID(ctx, "open", "done", "qa", false, admin())
	require.NoError(t, err)
	assert.Equal(t, []string{
		"StatusRepository.FindByID(open)",
		"StatusRepository.FindByID(done)",
		"LifecycleRepository.GetLifecycle",
		"RoleRepository.FindByID(qa)",
		"LifecycleRepository.Save",
	}, f.log.entries)
	assert.Equal(t, "qa", rule.GetAllowedRoles()[0].ID)

	f = newLCFixture()
	_, err = f.svc.SetTransitionRulesByID(ctx, "nope", "done", "qa", false, admin())
	assert.Contains(t, fieldsOf(t, err), "fromId")
	assert.Nil(t, f.lifecycles.saved)
}

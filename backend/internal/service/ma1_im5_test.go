// Tests for MA-1 (register) and IM-5 (deleteIssue).
//
// Part 1: unit tests of the domain classes involved (Sprint, User, Role).
// Part 2: service tests with mocked repositories. The mocks write every call into a shared log, so the tests
//
//	can check the exact call sequence of the sequence diagrams.
//
// External test package: it only uses the exported API and cannot clash with other test files.
package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/maket12/issues-tracker/backend/internal/domain"
	"github.com/maket12/issues-tracker/backend/internal/service"
)

var ctx = context.Background()

func day(s string) domain.Date {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return domain.Date{Time: t}
}

func userWith(id string, perms ...domain.Permission) *domain.User {
	return &domain.User{ID: id, Name: id, Role: &domain.Role{ID: "r", Permissions: perms}}
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var ve *domain.ValidationError
	require.ErrorAs(t, err, &ve)
	return ve.Fields
}

// ================================================================ part 1: domain unit tests

func TestSprint_ContainsAndRemoveIssue(t *testing.T) {
	i1, i2, i3 := &domain.Issue{ID: "ISS-1"}, &domain.Issue{ID: "ISS-2"}, &domain.Issue{ID: "ISS-3"}
	sprint := domain.RestoreSprint("SPR-1", "Sprint 1", day("2026-11-02"), nil, false, []*domain.Issue{i1, i2, i3})

	assert.True(t, sprint.Contains(&domain.Issue{ID: "ISS-2"}), "issues are compared by id, not by pointer")
	assert.False(t, sprint.Contains(&domain.Issue{ID: "ISS-9"}))
	assert.False(t, sprint.Contains(nil))
	assert.False(t, sprint.Contains(&domain.Issue{}), "an unsaved issue (no id) is never in a sprint")

	sprint.RemoveIssue(&domain.Issue{ID: "ISS-2"})
	assert.Equal(t, []*domain.Issue{i1, i3}, sprint.GetIssues(), "only that issue is removed, order is kept")

	sprint.RemoveIssue(&domain.Issue{ID: "ISS-2"}) // already gone
	sprint.RemoveIssue(nil)
	assert.Len(t, sprint.GetIssues(), 2, "removing an absent issue is a no-op")
}

func TestSprint_GetIssuesReturnsACopy(t *testing.T) {
	i1 := &domain.Issue{ID: "ISS-1"}
	sprint := domain.RestoreSprint("SPR-1", "Sprint 1", day("2026-11-02"), nil, false, []*domain.Issue{i1})

	got := sprint.GetIssues()
	got[0] = &domain.Issue{ID: "ISS-99"}
	assert.True(t, sprint.Contains(i1))
}

func TestNewSprint_StartDate(t *testing.T) {
	// 01:30 on 10 Nov in UTC+3 is still 9 Nov in UTC
	now := time.Date(2026, 11, 10, 1, 30, 0, 0, time.FixedZone("UTC+3", 3*3600))

	assert.Equal(t, "2026-11-09", domain.NewSprint("S", domain.Date{}, now).StartDate().Format("2006-01-02"), "zero start date means today (UTC)")
	assert.Equal(t, "2026-12-01", domain.NewSprint("S", day("2026-12-01"), now).StartDate().Format("2006-01-02"), "an explicit start date is kept")

	s := domain.NewSprint("S", day("2026-12-01"), now)
	assert.Equal(t, "S", s.Name())
	assert.False(t, s.IsClosed())
	s.Close()
	assert.True(t, s.IsClosed())
}

func TestNewUser(t *testing.T) {
	role := &domain.Role{ID: "user"}

	u, err := domain.NewUser("  Bob ", " BOB@Example.com ", "hash", role)
	require.NoError(t, err)
	assert.Equal(t, "Bob", u.Name)
	assert.Equal(t, "bob@example.com", u.GetEmail(), "e-mail is normalised")
	assert.Equal(t, "hash", u.PasswdHash)
	assert.Same(t, role, u.Role)
	assert.Empty(t, u.ID, "the id is assigned on save")

	for name, c := range map[string]struct{ name, email, field string }{
		"blank name":              {"  ", "bob@example.com", "name"},
		"name too long":           {strings.Repeat("я", 101), "bob@example.com", "name"},
		"empty e-mail":            {"Bob", "", "email"},
		"e-mail without @":        {"Bob", "bob.example.com", "email"},
		"e-mail with a name part": {"Bob", "Bob <bob@example.com>", "email"},
		"e-mail too long":         {"Bob", strings.Repeat("a", 250) + "@example.com", "email"},
	} {
		_, err := domain.NewUser(c.name, c.email, "hash", role)
		assert.Contains(t, fieldsOf(t, err), c.field, name)
	}

	_, err = domain.NewUser("Bob", "bob@example.com", "hash", nil)
	assert.Error(t, err, "a user needs a role")
	_, err = domain.NewUser("Bob", "bob@example.com", "", role)
	assert.Error(t, err, "a user needs a password hash")
}

func TestRole_GetPermissionsReturnsACopy(t *testing.T) {
	role := &domain.Role{Permissions: []domain.Permission{domain.CreateIssue}}
	perms := role.GetPermissions()
	perms[0] = domain.DeleteIssue

	assert.True(t, role.HasPermission(domain.CreateIssue))
	assert.False(t, role.HasPermission(domain.DeleteIssue))
	assert.Nil(t, (*domain.Role)(nil).GetPermissions())
}

func TestBcryptHasher(t *testing.T) {
	hash, err := service.BcryptHasher{Cost: bcrypt.MinCost}.Hash("s3cretpass1")
	require.NoError(t, err)
	assert.NotContains(t, hash, "s3cretpass1")
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte("s3cretpass1")))
	assert.Error(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte("wrong-pass1")))
}

// ================================================================ part 2: mocks

type callLog struct{ entries []string }

func (c *callLog) add(format string, args ...any) {
	c.entries = append(c.entries, fmt.Sprintf(format, args...))
}

var (
	_ service.IssueRepository  = (*mockIssueRepo)(nil)
	_ service.SprintRepository = (*mockSprintRepo)(nil)
	_ service.UserRepository   = (*mockUserRepo)(nil)
	_ service.RoleRepository   = (*mockRoleRepo)(nil)
	_ service.PasswordHasher   = (*stubHasher)(nil)
)

// ---- IssueRepository: only FindByID and Delete are part of IM-5; any other call is logged as UNEXPECTED

type mockIssueRepo struct {
	log       *callLog
	issue     *domain.Issue // returned by FindByID; nil = no such issue
	findErr   error
	deleted   bool
	deleteErr error
}

func (m *mockIssueRepo) FindByID(_ context.Context, id string) (*domain.Issue, error) {
	m.log.add("IssueRepository.FindByID(%s)", id)
	if m.findErr != nil {
		return nil, m.findErr
	}
	if m.issue == nil {
		return nil, domain.ErrNotFound
	}
	return m.issue, nil
}

func (m *mockIssueRepo) Delete(_ context.Context, issue *domain.Issue) (bool, error) {
	m.log.add("IssueRepository.Delete(%s)", issue.ID)
	return m.deleted, m.deleteErr
}

func (m *mockIssueRepo) unexpected(name string) error {
	m.log.add("UNEXPECTED IssueRepository.%s", name)
	return errors.New("unexpected call: " + name)
}
func (m *mockIssueRepo) FindTypeByID(context.Context, string) (*domain.IssueType, error) {
	return nil, m.unexpected("FindTypeByID")
}
func (m *mockIssueRepo) FindPriorityByID(context.Context, string) (*domain.Priority, error) {
	return nil, m.unexpected("FindPriorityByID")
}
func (m *mockIssueRepo) FindUserByID(context.Context, string) (*domain.User, error) {
	return nil, m.unexpected("FindUserByID")
}
func (m *mockIssueRepo) FindInitialStatus(context.Context) (*domain.Status, error) {
	return nil, m.unexpected("FindInitialStatus")
}
func (m *mockIssueRepo) Save(context.Context, *domain.Issue) (*domain.Issue, error) {
	return nil, m.unexpected("Save")
}

// ---- SprintRepository

type mockSprintRepo struct {
	log     *callLog
	sprint  *domain.Sprint // returned by FindByIssue; nil = the issue is in no sprint
	findErr error
	saveErr error
	saved   *domain.Sprint
}

func (m *mockSprintRepo) FindByIssue(_ context.Context, issue *domain.Issue) (*domain.Sprint, error) {
	m.log.add("SprintRepository.FindByIssue(%s)", issue.ID)
	if m.findErr != nil {
		return nil, m.findErr
	}
	if m.sprint == nil {
		return nil, domain.ErrNotFound
	}
	return m.sprint, nil
}

func (m *mockSprintRepo) Save(_ context.Context, s *domain.Sprint) (*domain.Sprint, error) {
	m.log.add("SprintRepository.Save(%s)", s.ID)
	if m.saveErr != nil {
		return nil, m.saveErr
	}
	m.saved = s
	return s, nil
}

// ---- UserRepository, RoleRepository, PasswordHasher

type mockUserRepo struct {
	log       *callLog
	exists    bool
	existsErr error
	saveErr   error
	saved     *domain.User // the user exactly as the service handed it to Save
}

func (m *mockUserRepo) ExistsByEmail(_ context.Context, email string) (bool, error) {
	m.log.add("UserRepository.ExistsByEmail(%s)", email)
	return m.exists, m.existsErr
}

func (m *mockUserRepo) Save(_ context.Context, u *domain.User) (*domain.User, error) {
	m.log.add("UserRepository.Save(%s)", u.GetEmail())
	if m.saveErr != nil {
		return nil, m.saveErr
	}
	m.saved = u
	registered := *u
	registered.ID = "USR-1" // what a real repository does
	return &registered, nil
}

type mockRoleRepo struct {
	log  *callLog
	role *domain.Role // nil = no default role
	err  error
}

func (m *mockRoleRepo) FindDefaultRole(context.Context) (*domain.Role, error) {
	m.log.add("RoleRepository.FindDefaultRole")
	if m.err != nil {
		return nil, m.err
	}
	if m.role == nil {
		return nil, domain.ErrNotFound
	}
	return m.role, nil
}

func (m *mockUserRepo) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	m.log.add("UNEXPECTED UserRepository.FindByEmail(%s)", email)
	return nil, errors.New("unexpected call: FindByEmail")
}

func (m *mockUserRepo) FindByID(_ context.Context, id string) (*domain.User, error) {
	m.log.add("UNEXPECTED UserRepository.FindByID(%s)", id)
	return nil, errors.New("unexpected call: FindByID")
}

func (m *mockRoleRepo) FindByID(_ context.Context, id string) (*domain.Role, error) {
	m.log.add("UNEXPECTED RoleRepository.FindByID(%s)", id)
	return nil, errors.New("unexpected call: FindByID")
}

type stubHasher struct {
	log *callLog
	err error
}

func (h *stubHasher) Hash(password string) (string, error) {
	h.log.add("PasswordHasher.Hash")
	if h.err != nil {
		return "", h.err
	}
	return "hashed:" + password, nil
}

// ================================================================ IM-5 deleteIssue

type deleteFixture struct {
	log     *callLog
	issues  *mockIssueRepo
	sprints *mockSprintRepo
	svc     *service.IssueService
}

func newDeleteFixture(issue *domain.Issue, sprint *domain.Sprint) *deleteFixture {
	log := &callLog{}
	issues := &mockIssueRepo{log: log, issue: issue, deleted: true}
	sprints := &mockSprintRepo{log: log, sprint: sprint}
	return &deleteFixture{log: log, issues: issues, sprints: sprints, svc: service.NewIssueService(issues, sprints)}
}

func sprintWith(issues ...*domain.Issue) *domain.Sprint {
	return domain.RestoreSprint("SPR-1", "Sprint 1", day("2026-11-02"), nil, false, issues)
}

func deleter() *domain.User { return userWith("root", domain.DeleteIssue) }

func TestDeleteIssue_IssueInSprint_FollowsTheSequenceDiagram(t *testing.T) {
	issue, other := &domain.Issue{ID: "ISS-1"}, &domain.Issue{ID: "ISS-2"}
	f := newDeleteFixture(issue, sprintWith(issue, other))

	confirmed, err := f.svc.DeleteIssue(ctx, "ISS-1", deleter())

	require.NoError(t, err)
	assert.True(t, confirmed)
	assert.Equal(t, []string{
		"IssueRepository.FindByID(ISS-1)",
		"SprintRepository.FindByIssue(ISS-1)",
		"SprintRepository.Save(SPR-1)", // the sprint is updated before the issue disappears
		"IssueRepository.Delete(ISS-1)",
	}, f.log.entries)
	require.NotNil(t, f.sprints.saved)
	assert.False(t, f.sprints.saved.Contains(issue), "the issue was removed from the sprint")
	assert.True(t, f.sprints.saved.Contains(other), "other issues stay")
}

func TestDeleteIssue_IssueWithoutSprint(t *testing.T) {
	f := newDeleteFixture(&domain.Issue{ID: "ISS-1"}, nil)

	confirmed, err := f.svc.DeleteIssue(ctx, "ISS-1", deleter())

	require.NoError(t, err)
	assert.True(t, confirmed)
	assert.Equal(t, []string{
		"IssueRepository.FindByID(ISS-1)",
		"SprintRepository.FindByIssue(ISS-1)",
		"IssueRepository.Delete(ISS-1)", // no sprint to save
	}, f.log.entries)
}

func TestDeleteIssue_SprintThatDoesNotListTheIssueIsLeftAlone(t *testing.T) {
	f := newDeleteFixture(&domain.Issue{ID: "ISS-1"}, sprintWith(&domain.Issue{ID: "ISS-2"}))

	_, err := f.svc.DeleteIssue(ctx, "ISS-1", deleter())

	require.NoError(t, err)
	assert.NotContains(t, f.log.entries, "SprintRepository.Save(SPR-1)")
	assert.Contains(t, f.log.entries, "IssueRepository.Delete(ISS-1)")
}

func TestDeleteIssue_PermissionIsCheckedBeforeAnythingElse(t *testing.T) {
	f := newDeleteFixture(&domain.Issue{ID: "ISS-1"}, nil)

	confirmed, err := f.svc.DeleteIssue(ctx, "ISS-1", nil)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)
	assert.False(t, confirmed)

	// the author of an issue, even with every other right, may not delete without DELETE_ISSUE
	author := userWith("alice", domain.CreateIssue, domain.ViewIssue, domain.EditIssue)
	confirmed, err = f.svc.DeleteIssue(ctx, "ISS-1", author)
	assert.ErrorIs(t, err, domain.ErrForbidden)
	assert.False(t, confirmed)

	assert.Empty(t, f.log.entries, "no repository is touched for a caller without the permission")
}

func TestDeleteIssue_UnknownIssue(t *testing.T) {
	f := newDeleteFixture(nil, nil)

	confirmed, err := f.svc.DeleteIssue(ctx, "ISS-404", deleter())

	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.False(t, confirmed)
	assert.Equal(t, []string{"IssueRepository.FindByID(ISS-404)"}, f.log.entries)
}

func TestDeleteIssue_AFailureStopsTheFlow(t *testing.T) {
	boom := errors.New("db down")
	findIssue, findSprint := "IssueRepository.FindByID(ISS-1)", "SprintRepository.FindByIssue(ISS-1)"
	saveSprint, deleteIssue := "SprintRepository.Save(SPR-1)", "IssueRepository.Delete(ISS-1)"

	tests := []struct {
		name    string
		setup   func(f *deleteFixture)
		wantErr error
		wantLog []string
	}{
		{"issue lookup fails", func(f *deleteFixture) { f.issues.findErr = boom }, boom,
			[]string{findIssue}},
		{"sprint lookup fails", func(f *deleteFixture) { f.sprints.findErr = boom }, boom,
			[]string{findIssue, findSprint}},
		{"sprint cannot be saved: the issue must not be deleted", func(f *deleteFixture) { f.sprints.saveErr = boom }, boom,
			[]string{findIssue, findSprint, saveSprint}},
		{"delete fails", func(f *deleteFixture) { f.issues.deleteErr = boom }, boom,
			[]string{findIssue, findSprint, saveSprint, deleteIssue}},
		{"issue was deleted by someone else meanwhile", func(f *deleteFixture) { f.issues.deleted = false }, domain.ErrNotFound,
			[]string{findIssue, findSprint, saveSprint, deleteIssue}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			issue := &domain.Issue{ID: "ISS-1"}
			f := newDeleteFixture(issue, sprintWith(issue))
			tc.setup(f)

			confirmed, err := f.svc.DeleteIssue(ctx, "ISS-1", deleter())

			assert.ErrorIs(t, err, tc.wantErr)
			assert.False(t, confirmed)
			assert.Equal(t, tc.wantLog, f.log.entries)
		})
	}
}

// ================================================================ MA-1 register

type registerFixture struct {
	log    *callLog
	users  *mockUserRepo
	roles  *mockRoleRepo
	hasher *stubHasher
	svc    *service.AuthService
}

func newRegisterFixture() *registerFixture {
	log := &callLog{}
	users := &mockUserRepo{log: log}
	roles := &mockRoleRepo{log: log, role: &domain.Role{
		ID: "user", Name: "User", Permissions: []domain.Permission{domain.CreateIssue, domain.ViewIssue},
	}}
	hasher := &stubHasher{log: log}
	return &registerFixture{log: log, users: users, roles: roles, hasher: hasher, svc: service.NewAuthService(users, roles, hasher)}
}

func TestRegister_FollowsTheSequenceDiagram(t *testing.T) {
	f := newRegisterFixture()

	registered, err := f.svc.Register(ctx, "  Bob ", " Bob@Example.COM ", "s3cretpass1")

	require.NoError(t, err)
	assert.Equal(t, []string{
		"UserRepository.ExistsByEmail(bob@example.com)", // the e-mail is normalised before the lookup
		"RoleRepository.FindDefaultRole",
		"PasswordHasher.Hash",
		"UserRepository.Save(bob@example.com)",
	}, f.log.entries)

	saved := f.users.saved
	require.NotNil(t, saved)
	assert.Equal(t, "Bob", saved.Name)
	assert.Equal(t, "bob@example.com", saved.Email)
	assert.Equal(t, "hashed:s3cretpass1", saved.PasswdHash)
	assert.NotEqual(t, "s3cretpass1", saved.PasswdHash, "the plain password is never stored")
	assert.Same(t, f.roles.role, saved.Role, "a new user gets the default role")

	assert.Equal(t, "USR-1", registered.ID, "the result is what the repository returned")
	assert.True(t, registered.HasPermission(domain.CreateIssue), "permissions of the default role are available")
	assert.False(t, registered.HasPermission(domain.DeleteIssue))
}

func TestRegister_RejectsInvalidInputWithoutTouchingAnything(t *testing.T) {
	tests := []struct {
		name, userName, email, password string
		fields                          []string
	}{
		{"blank name", "  ", "bob@example.com", "s3cretpass1", []string{"name"}},
		{"bad e-mail", "Bob", "bob-at-example.com", "s3cretpass1", []string{"email"}},
		{"password too short", "Bob", "bob@example.com", "abc123", []string{"password"}},
		{"password without digits", "Bob", "bob@example.com", "abcdefghij", []string{"password"}},
		{"password without letters", "Bob", "bob@example.com", "1234567890", []string{"password"}},
		{"password too long for bcrypt", "Bob", "bob@example.com", strings.Repeat("a1", 37), []string{"password"}},
		{"everything wrong at once", "", "nope", "x", []string{"name", "email", "password"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newRegisterFixture()
			_, err := f.svc.Register(ctx, tc.userName, tc.email, tc.password)

			fields := fieldsOf(t, err)
			for _, field := range tc.fields {
				assert.Contains(t, fields, field)
			}
			assert.Empty(t, f.log.entries, "nothing is looked up, hashed or saved")
		})
	}
}

func TestRegister_EmailAlreadyRegistered(t *testing.T) {
	f := newRegisterFixture()
	f.users.exists = true

	_, err := f.svc.Register(ctx, "Bob", "bob@example.com", "s3cretpass1")

	assert.ErrorIs(t, err, domain.ErrEmailTaken)
	assert.Equal(t, []string{"UserRepository.ExistsByEmail(bob@example.com)"}, f.log.entries,
		"no role lookup, no (slow) hashing, no save")
}

func TestRegister_LosingARaceOnSaveStillReportsEmailTaken(t *testing.T) {
	f := newRegisterFixture()
	f.users.saveErr = domain.ErrEmailTaken // the unique index fired after existsByEmail said "free"

	_, err := f.svc.Register(ctx, "Bob", "bob@example.com", "s3cretpass1")

	assert.ErrorIs(t, err, domain.ErrEmailTaken)
}

func TestRegister_DefaultRoleProblems(t *testing.T) {
	t.Run("no default role configured", func(t *testing.T) {
		f := newRegisterFixture()
		f.roles.role = nil

		_, err := f.svc.Register(ctx, "Bob", "bob@example.com", "s3cretpass1")

		assert.ErrorIs(t, err, domain.ErrNoDefaultRole)
		assert.Equal(t, []string{"UserRepository.ExistsByEmail(bob@example.com)", "RoleRepository.FindDefaultRole"}, f.log.entries)
	})
	t.Run("default role that can assign roles is refused", func(t *testing.T) {
		f := newRegisterFixture()
		f.roles.role.Permissions = append(f.roles.role.Permissions, domain.ManageRoles)

		_, err := f.svc.Register(ctx, "Bob", "bob@example.com", "s3cretpass1")

		assert.ErrorIs(t, err, domain.ErrUnsafeDefaultRole)
		assert.NotContains(t, f.log.entries, "PasswordHasher.Hash")
		assert.Nil(t, f.users.saved, "nobody registers as an administrator")
	})
}

func TestRegister_InfrastructureFailuresStopTheFlow(t *testing.T) {
	boom := errors.New("db down")
	exists, role := "UserRepository.ExistsByEmail(bob@example.com)", "RoleRepository.FindDefaultRole"
	hash, save := "PasswordHasher.Hash", "UserRepository.Save(bob@example.com)"

	tests := []struct {
		name    string
		setup   func(f *registerFixture)
		wantLog []string
	}{
		{"e-mail check fails", func(f *registerFixture) { f.users.existsErr = boom }, []string{exists}},
		{"role lookup fails", func(f *registerFixture) { f.roles.err = boom }, []string{exists, role}},
		{"hashing fails", func(f *registerFixture) { f.hasher.err = boom }, []string{exists, role, hash}},
		{"save fails", func(f *registerFixture) { f.users.saveErr = boom }, []string{exists, role, hash, save}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newRegisterFixture()
			tc.setup(f)

			registered, err := f.svc.Register(ctx, "Bob", "bob@example.com", "s3cretpass1")

			assert.ErrorIs(t, err, boom)
			assert.Nil(t, registered)
			assert.Equal(t, tc.wantLog, f.log.entries)
		})
	}
}

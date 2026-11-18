// Tests for MA-2 (login / logout) and MA-3 (assignRole).
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/maket12/issues-tracker/backend/internal/domain"
	"github.com/maket12/issues-tracker/backend/internal/service"
)

// ================================================================ part 1: domain unit tests for MA-2 & MA-3

func TestUser_CheckPasswdAndRoleMethods(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("s3cretpass1"), bcrypt.MinCost)
	require.NoError(t, err)

	userRole := &domain.Role{ID: "user", Name: "User", Permissions: []domain.Permission{domain.ViewIssue}}
	qaRole := &domain.Role{ID: "qa", Name: "QA Role", Permissions: []domain.Permission{domain.ViewIssue, domain.EditIssue}}

	u := &domain.User{ID: "USR-1", Name: "Bob", Email: "bob@example.com", PasswdHash: string(hash), Role: userRole}

	assert.Equal(t, "USR-1", u.GetID())
	assert.Same(t, userRole, u.GetRole())
	assert.True(t, u.IsPrivileged())
	assert.True(t, u.CheckPasswd("s3cretpass1"))
	assert.False(t, u.CheckPasswd("wrong-password"))
	assert.False(t, u.CheckPasswd(""))
	assert.False(t, (*domain.User)(nil).CheckPasswd("s3cretpass1"))

	u.SetRole(qaRole)
	assert.Same(t, qaRole, u.GetRole())
}

// ================================================================ part 2: mocks for MA-2 & MA-3

type ma23UserRepo struct {
	log       *callLog
	byEmail   map[string]*domain.User
	byID      map[string]*domain.User
	findErr   error
	saveErr   error
	savedUser *domain.User
}

func (m *ma23UserRepo) ExistsByEmail(_ context.Context, email string) (bool, error) {
	m.log.add("UNEXPECTED UserRepository.ExistsByEmail(%s)", email)
	return false, errors.New("unexpected call")
}

func (m *ma23UserRepo) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	m.log.add("UserRepository.FindByEmail(%s)", email)
	if m.findErr != nil {
		return nil, m.findErr
	}
	u, ok := m.byEmail[email]
	if !ok || u == nil {
		return nil, domain.ErrNotFound
	}
	c := *u
	return &c, nil
}

func (m *ma23UserRepo) FindByID(_ context.Context, id string) (*domain.User, error) {
	m.log.add("UserRepository.FindByID(%s)", id)
	if m.findErr != nil {
		return nil, m.findErr
	}
	u, ok := m.byID[id]
	if !ok || u == nil {
		return nil, domain.ErrNotFound
	}
	c := *u
	return &c, nil
}

func (m *ma23UserRepo) Save(_ context.Context, u *domain.User) (*domain.User, error) {
	m.log.add("UserRepository.Save(%s)", u.GetID())
	if m.saveErr != nil {
		return nil, m.saveErr
	}
	m.savedUser = u
	out := *u
	return &out, nil
}

type ma23RoleRepo struct {
	log     *callLog
	byID    map[string]*domain.Role
	findErr error
}

func (m *ma23RoleRepo) FindDefaultRole(context.Context) (*domain.Role, error) {
	m.log.add("UNEXPECTED RoleRepository.FindDefaultRole")
	return nil, errors.New("unexpected call")
}

func (m *ma23RoleRepo) FindByID(_ context.Context, id string) (*domain.Role, error) {
	m.log.add("RoleRepository.FindByID(%s)", id)
	if m.findErr != nil {
		return nil, m.findErr
	}
	r, ok := m.byID[id]
	if !ok || r == nil {
		return nil, domain.ErrNotFound
	}
	return r, nil
}

type mockSessionRepo struct {
	log       *callLog
	saveErr   error
	deleteErr error
	closed    bool
	saved     *domain.Session
}

func (m *mockSessionRepo) SaveSession(_ context.Context, u *domain.User, r *domain.Role) (*domain.Session, error) {
	m.log.add("SessionRepository.SaveSession(%s, %s)", u.GetID(), r.ID)
	if m.saveErr != nil {
		return nil, m.saveErr
	}
	sess := &domain.Session{ID: "SES-1", User: u, Role: r}
	m.saved = sess
	return sess, nil
}

func (m *mockSessionRepo) DeleteByUserID(_ context.Context, userID string) (bool, error) {
	m.log.add("SessionRepository.DeleteByUserID(%s)", userID)
	if m.deleteErr != nil {
		return false, m.deleteErr
	}
	return m.closed, nil
}

// ================================================================ MA-2 login & logout

type authFixture struct {
	log      *callLog
	users    *ma23UserRepo
	roles    *ma23RoleRepo
	sessions *mockSessionRepo
	svc      *service.AuthService
}

func newAuthFixture() *authFixture {
	log := &callLog{}
	role := &domain.Role{ID: "user", Name: "User", Permissions: []domain.Permission{domain.ViewIssue}}
	bob := &domain.User{
		ID: "USR-1", Name: "Bob", Email: "bob@example.com",
		PasswdHash: "hashed:s3cretpass1", Role: role,
	}
	users := &ma23UserRepo{
		log:     log,
		byEmail: map[string]*domain.User{"bob@example.com": bob},
		byID:    map[string]*domain.User{"USR-1": bob},
	}
	roles := &ma23RoleRepo{log: log, byID: map[string]*domain.Role{"user": role}}
	sessions := &mockSessionRepo{log: log, closed: true}
	svc := service.NewAuthService(users, roles, &stubHasher{log: log}, sessions)
	return &authFixture{log: log, users: users, roles: roles, sessions: sessions, svc: svc}
}

func TestLogin_FollowsTheSequenceDiagram(t *testing.T) {
	f := newAuthFixture()

	sess, err := f.svc.Login(ctx, "  Bob@Example.COM ", "s3cretpass1")

	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, "SES-1", sess.ID)
	assert.Equal(t, "USR-1", sess.User.GetID())
	assert.Equal(t, "user", sess.Role.ID)
	assert.Equal(t, []string{
		"UserRepository.FindByEmail(bob@example.com)",
		"SessionRepository.SaveSession(USR-1, user)",
	}, f.log.entries)
}

func TestLogin_UnsuccessfulFlows(t *testing.T) {
	t.Run("unknown email", func(t *testing.T) {
		f := newAuthFixture()
		_, err := f.svc.Login(ctx, "ghost@example.com", "s3cretpass1")
		assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
		assert.Equal(t, []string{"UserRepository.FindByEmail(ghost@example.com)"}, f.log.entries)
	})

	t.Run("wrong password", func(t *testing.T) {
		f := newAuthFixture()
		_, err := f.svc.Login(ctx, "bob@example.com", "wrong-pass")
		assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
		assert.Equal(t, []string{"UserRepository.FindByEmail(bob@example.com)"}, f.log.entries, "no session is saved")
	})

	t.Run("role without VIEW_ISSUE permission is denied", func(t *testing.T) {
		f := newAuthFixture()
		f.users.byEmail["bob@example.com"].Role = &domain.Role{ID: "none", Permissions: nil}
		_, err := f.svc.Login(ctx, "bob@example.com", "s3cretpass1")
		assert.ErrorIs(t, err, domain.ErrForbidden)
		assert.Equal(t, []string{"UserRepository.FindByEmail(bob@example.com)"}, f.log.entries)
	})

	t.Run("empty credentials", func(t *testing.T) {
		f := newAuthFixture()
		_, err := f.svc.Login(ctx, "  ", "s3cretpass1")
		assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
		assert.Empty(t, f.log.entries)
	})
}

func TestLogout_FollowsTheSequenceDiagram(t *testing.T) {
	f := newAuthFixture()
	bob := f.users.byID["USR-1"]

	closed, err := f.svc.Logout(ctx, bob)

	require.NoError(t, err)
	assert.True(t, closed)
	assert.Equal(t, []string{
		"SessionRepository.DeleteByUserID(USR-1)",
	}, f.log.entries)
}

func TestLogout_RequiresAuthenticatedUser(t *testing.T) {
	f := newAuthFixture()

	closed, err := f.svc.Logout(ctx, nil)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)
	assert.False(t, closed)
	assert.Empty(t, f.log.entries)
}

// ================================================================ MA-3 assignRole

type roleFixture struct {
	log   *callLog
	users *ma23UserRepo
	roles *ma23RoleRepo
	svc   *service.RoleService
}

func newRoleFixture() *roleFixture {
	log := &callLog{}
	userRole := &domain.Role{ID: "user", Name: "User", Permissions: []domain.Permission{domain.ViewIssue}}
	qaRole := &domain.Role{ID: "qa", Name: "QA Role", Permissions: []domain.Permission{domain.ViewIssue, domain.EditIssue}}
	target := &domain.User{ID: "USR-2", Name: "Anisimov", Email: "v.anisimov@g.nsu.ru", Role: userRole}

	users := &ma23UserRepo{
		log:  log,
		byID: map[string]*domain.User{"USR-2": target},
	}
	roles := &ma23RoleRepo{
		log:  log,
		byID: map[string]*domain.Role{"user": userRole, "qa": qaRole},
	}
	return &roleFixture{log: log, users: users, roles: roles, svc: service.NewRoleService(roles, users)}
}

func roleAdmin() *domain.User {
	return userWith("USR-1", domain.ManageRoles)
}

func TestAssignRole_FollowsTheSequenceDiagram(t *testing.T) {
	f := newRoleFixture()

	updated, err := f.svc.AssignRole(ctx, roleAdmin(), "USR-2", "qa")

	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "USR-2", updated.GetID())
	assert.Equal(t, "qa", updated.GetRole().ID)
	assert.Equal(t, []string{
		"UserRepository.FindByID(USR-2)",
		"RoleRepository.FindByID(qa)",
		"UserRepository.Save(USR-2)",
	}, f.log.entries)
	require.NotNil(t, f.users.savedUser)
	assert.Equal(t, "qa", f.users.savedUser.GetRole().ID)
}

func TestAssignRole_PermissionIsCheckedFirst(t *testing.T) {
	f := newRoleFixture()

	_, err := f.svc.AssignRole(ctx, nil, "USR-2", "qa")
	assert.ErrorIs(t, err, domain.ErrUnauthorized)

	regularUser := userWith("USR-3", domain.CreateIssue, domain.ViewIssue)
	_, err = f.svc.AssignRole(ctx, regularUser, "USR-2", "qa")
	assert.ErrorIs(t, err, domain.ErrForbidden)

	unprivileged := &domain.User{ID: "USR-4", Role: &domain.Role{ID: "empty"}}
	_, err = f.svc.AssignRole(ctx, unprivileged, "USR-2", "qa")
	assert.ErrorIs(t, err, domain.ErrForbidden)

	assert.Empty(t, f.log.entries, "no repository is touched when admin lacks MANAGE_ROLES")
}

func TestAssignRole_MissingUserOrRole(t *testing.T) {
	t.Run("target user not found", func(t *testing.T) {
		f := newRoleFixture()
		_, err := f.svc.AssignRole(ctx, roleAdmin(), "USR-404", "qa")
		assert.ErrorIs(t, err, domain.ErrNotFound)
		assert.Equal(t, []string{"UserRepository.FindByID(USR-404)"}, f.log.entries)
	})

	t.Run("new role not found", func(t *testing.T) {
		f := newRoleFixture()
		_, err := f.svc.AssignRole(ctx, roleAdmin(), "USR-2", "no-such-role")
		assert.ErrorIs(t, err, domain.ErrNotFound)
		assert.Equal(t, []string{
			"UserRepository.FindByID(USR-2)",
			"RoleRepository.FindByID(no-such-role)",
		}, f.log.entries)
	})
}

func TestAssignRole_InfrastructureFailuresStopTheFlow(t *testing.T) {
	boom := errors.New("db down")

	t.Run("user lookup fails", func(t *testing.T) {
		f := newRoleFixture()
		f.users.findErr = boom
		_, err := f.svc.AssignRole(ctx, roleAdmin(), "USR-2", "qa")
		assert.ErrorIs(t, err, boom)
		assert.Equal(t, []string{"UserRepository.FindByID(USR-2)"}, f.log.entries)
	})

	t.Run("role lookup fails", func(t *testing.T) {
		f := newRoleFixture()
		f.roles.findErr = boom
		_, err := f.svc.AssignRole(ctx, roleAdmin(), "USR-2", "qa")
		assert.ErrorIs(t, err, boom)
		assert.Equal(t, []string{"UserRepository.FindByID(USR-2)", "RoleRepository.FindByID(qa)"}, f.log.entries)
	})

	t.Run("save fails", func(t *testing.T) {
		f := newRoleFixture()
		f.users.saveErr = boom
		_, err := f.svc.AssignRole(ctx, roleAdmin(), "USR-2", "qa")
		assert.ErrorIs(t, err, boom)
		assert.Equal(t, []string{
			"UserRepository.FindByID(USR-2)",
			"RoleRepository.FindByID(qa)",
			"UserRepository.Save(USR-2)",
		}, f.log.entries)
	})
}

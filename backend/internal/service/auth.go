package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// PasswordHasher turns a plain password into the hash that is stored in Users.passwdHash.
type PasswordHasher interface {
	Hash(password string) (string, error)
}

// BcryptHasher is the production PasswordHasher. Cost 0 means bcrypt.DefaultCost.
type BcryptHasher struct{ Cost int }

func (h BcryptHasher) Hash(password string) (string, error) {
	cost := h.Cost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// AuthService is the AuthService of the class diagram (only register so far; login/logout belong to MA-2).
type AuthService struct {
	users    UserRepository
	roles    RoleRepository
	sessions SessionRepository
	hasher   PasswordHasher
}

func NewAuthService(users UserRepository, roles RoleRepository, hasher PasswordHasher, sessions ...SessionRepository) *AuthService {
	var sess SessionRepository
	if len(sessions) > 0 {
		sess = sessions[0]
	}
	return &AuthService{users: users, roles: roles, sessions: sess, hasher: hasher}
}

// Register implements MA-1: register(name, email, password).
func (s *AuthService) Register(ctx context.Context, name, email, password string) (*domain.User, error) {
	// Input is validated first so that nothing is looked up or hashed for a request that cannot succeed.
	problems := map[string]string{}
	name, email = domain.NormaliseProfile(name, email, problems)
	if !strongPassword(password) {
		problems["password"] = "must be 8-72 characters and contain letters and digits"
	}
	if len(problems) > 0 {
		return nil, &domain.ValidationError{Fields: problems}
	}

	// UserRepository.existsByEmail(email) -> false
	taken, err := s.users.ExistsByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("check e-mail: %w", err)
	}
	if taken {
		return nil, domain.ErrEmailTaken
	}

	// RoleRepository.findDefaultRole() -> defaultRole
	defaultRole, err := s.roles.FindDefaultRole(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrNoDefaultRole
	}
	if err != nil {
		return nil, fmt.Errorf("find default role: %w", err)
	}

	// defaultRole.getPermissions() -> permissionsList
	// Guard: registering must never hand out the right to assign roles.
	if slices.Contains(defaultRole.GetPermissions(), domain.ManageRoles) {
		return nil, domain.ErrUnsafeDefaultRole
	}

	passwdHash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	// new User(name, email, passwdHash, defaultRole)
	newUser, err := domain.NewUser(name, email, passwdHash, defaultRole)
	if err != nil {
		return nil, err
	}

	// newUser.getEmail() -> email, then UserRepository.save(newUser) -> registeredUser
	registered, err := s.users.Save(ctx, newUser)
	if errors.Is(err, domain.ErrEmailTaken) { // lost a race against another registration
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("save user %q: %w", newUser.GetEmail(), err)
	}
	return registered, nil
}

// Login implements MA-2 (Log in): login(email, password).
func (s *AuthService) Login(ctx context.Context, email, password string) (*domain.Session, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return nil, domain.ErrInvalidCredentials
	}

	// UserRepository.findByEmail(email) -> user
	user, err := s.users.FindByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("find user by e-mail: %w", err)
	}

	// user.checkPasswd(password) -> true
	if !user.CheckPasswd(password) {
		return nil, domain.ErrInvalidCredentials
	}

	// user.getRole() -> role
	role := user.GetRole()
	// role.hasPermission(VIEW_ISSUE) -> true
	if !role.HasPermission(domain.ViewIssue) {
		return nil, domain.ErrForbidden
	}

	// SessionRepository.saveSession(user, role) -> activeSession
	activeSession, err := s.sessions.SaveSession(ctx, user, role)
	if err != nil {
		return nil, fmt.Errorf("save session: %w", err)
	}
	return activeSession, nil
}

// Logout implements MA-2 (Log out): logout(user).
func (s *AuthService) Logout(ctx context.Context, user *domain.User) (bool, error) {
	if user == nil || strings.TrimSpace(user.GetID()) == "" {
		return false, domain.ErrUnauthorized
	}

	// user.getId() -> userId
	userID := user.GetID()

	// SessionRepository.deleteByUserId(userId) -> sessionClosed
	sessionClosed, err := s.sessions.DeleteByUserID(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("delete session: %w", err)
	}
	return sessionClosed, nil
}

// strongPassword: 8-72 bytes (bcrypt ignores everything after 72), at least one letter and one digit.
func strongPassword(p string) bool {
	if len(p) < 8 || len(p) > 72 {
		return false
	}
	var letter, digit bool
	for _, r := range p {
		switch {
		case unicode.IsLetter(r):
			letter = true
		case unicode.IsDigit(r):
			digit = true
		}
	}
	return letter && digit
}

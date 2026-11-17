package domain

import (
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Errors of MA-1 (register).
var (
	ErrEmailTaken        = errors.New("a user with this e-mail already exists")
	ErrNoDefaultRole     = errors.New("no default role is configured")
	ErrUnsafeDefaultRole = errors.New("the default role must not grant MANAGE_ROLES")
)

const (
	MaxUserNameLength = 100
	MaxEmailLength    = 255
)

type User struct {
	ID    string
	Name  string
	Email string
	Role  *Role
	PasswdHash string
}

func (u *User) HasPermission(p Permission) bool {
	return u != nil && u.Role.HasPermission(p)
}

func (u *User) GetEmail() string { return u.Email }

// NewUser is `new User(name, email, passwdHash, role)` from the MA-1 sequence diagram.
// The id stays empty until the user is saved. passwdHash is already hashed: the plain password
// never reaches the domain.
func NewUser(name, email, passwdHash string, role *Role) (*User, error) {
	if role == nil || passwdHash == "" {
		return nil, errors.New("domain: a user needs a password hash and a role")
	}
	problems := map[string]string{}
	name, email = NormaliseProfile(name, email, problems)
	if len(problems) > 0 {
		return nil, &ValidationError{Fields: problems}
	}
	return &User{Name: name, Email: email, PasswdHash: passwdHash, Role: role}, nil
}

// NormaliseProfile trims the name, lower-cases the e-mail, and records invalid values in problems.
func NormaliseProfile(name, email string, problems map[string]string) (string, string) {
	name = strings.TrimSpace(name)
	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		problems["name"] = "is required"
	case n > MaxUserNameLength:
		problems["name"] = "must be at most 100 characters"
	}

	email = strings.ToLower(strings.TrimSpace(email))
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		problems["email"] = "must be a valid e-mail address"
	} else if len(email) > MaxEmailLength {
		problems["email"] = "must be at most 255 characters"
	}
	return name, email
}

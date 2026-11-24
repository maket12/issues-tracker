package domain

import (
	"strings"
	"unicode/utf8"
)

const MaxStatusNameLength = 100

// NewStatus is `new Status(statusName)` from the LC-1 sequence diagram.
// The id stays empty until the status is saved.
func NewStatus(name string) (*Status, error) {
	name = strings.TrimSpace(name)
	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		return nil, NewValidation("name", "is required")
	case n > MaxStatusNameLength:
		return nil, NewValidation("name", "must be at most 100 characters")
	}
	return &Status{Name: name}, nil
}

func (s *Status) GetName() string {
	if s == nil {
		return ""
	}
	return s.Name
}

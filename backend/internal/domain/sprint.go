package domain

import (
	"slices"
	"time"
)

// Sprint is the Sprint class of the class diagram.
type Sprint struct {
	ID        string
	name      string
	startDate Date
	EndDate   *Date
	issues    []*Issue
	closed    bool
}

// NewSprint creates an open sprint without issues. A zero startDate means "today" (UTC, taken from now).
func NewSprint(name string, startDate Date, now time.Time) *Sprint {
	if startDate.IsZero() {
		n := now.UTC()
		startDate = Date{Time: time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)}
	}
	return &Sprint{
		name:      name,
		startDate: startDate,
		issues:    make([]*Issue, 0),
	}
}

// RestoreSprint rebuilds a stored sprint; SprintRepository implementations use it.
func RestoreSprint(id, name string, startDate Date, endDate *Date, closed bool, issues []*Issue) *Sprint {
	return &Sprint{
		ID:        id,
		name:      name,
		startDate: startDate,
		EndDate:   endDate,
		issues:    slices.Clone(issues),
		closed:    closed,
	}
}

func (s *Sprint) Name() string     { return s.name }
func (s *Sprint) StartDate() Date  { return s.startDate }
func (s *Sprint) IsClosed() bool   { return s.closed }
func (s *Sprint) Close()           { s.closed = true }

// GetIssues returns a copy, so callers cannot change the sprint through the slice.
func (s *Sprint) GetIssues() []*Issue {
	copied := make([]*Issue, len(s.issues))
	copy(copied, s.issues)
	return copied
}

func (s *Sprint) AddIssue(issue *Issue) {
	s.issues = append(s.issues, issue)
}

// Contains reports whether the issue (compared by id) is in the sprint.
func (s *Sprint) Contains(issue *Issue) bool {
	return s.indexOf(issue) >= 0
}

// RemoveIssue removes the issue from the sprint; it does nothing when the issue is not in it.
func (s *Sprint) RemoveIssue(issue *Issue) {
	if i := s.indexOf(issue); i >= 0 {
		s.issues = slices.Delete(s.issues, i, i+1)
	}
}

func (s *Sprint) indexOf(issue *Issue) int {
	if s == nil || issue == nil || issue.ID == "" { // an unsaved issue cannot be in a sprint
		return -1
	}
	for i, candidate := range s.issues {
		if candidate != nil && candidate.ID == issue.ID {
			return i
		}
	}
	return -1
}

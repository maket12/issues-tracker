package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxNameLength       = 200     // characters
	MaxDescriptionBytes = 1 << 20 // 1 MiB of rich text
)

type Issue struct {
	ID          string
	Name        string
	Type        *IssueType
	Description *string
	Author      *User
	Assignee    *User
	Status      *Status
	Priority    *Priority
	StartDate   Date
	DueDate     *Date
	Version     string
}

func NewIssue(name string, issueType *IssueType, description *string, author *User, status *Status, now time.Time) (*Issue, error) {
	if author == nil || status == nil {
		return nil, errors.New("domain: an issue needs an author and a status")
	}

	problems := map[string]string{}
	name, description = normaliseText(name, description, problems)

	if issueType == nil {
		problems["typeId"] = "is required"
	}

	if len(problems) > 0 {
		return nil, &ValidationError{Fields: problems}
	}

	n := now.UTC()

	return &Issue{
		Name: name, Type: issueType, Description: description, Author: author, Status: status,
		StartDate: Date{Time: time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)},
	}, nil
}

func (i *Issue) Edit(name string, issueType *IssueType, description *string, assignee *User,
	priority *Priority, startDate Date, dueDate *Date) error {

	problems := map[string]string{}
	name, description = normaliseText(name, description, problems)

	if issueType == nil {
		problems["typeId"] = "is required"
	}

	if priority == nil {
		problems["priorityId"] = "is required"
	}

	if startDate.IsZero() {
		problems["startDate"] = "is required"
	} else if dueDate != nil && dueDate.Before(startDate.Time) {
		problems["dueDate"] = "must not be earlier than startDate"
	}

	if len(problems) > 0 {
		return &ValidationError{Fields: problems}
	}

	i.Name, i.Type, i.Description, i.Assignee = name, issueType, description, assignee
	i.Priority, i.StartDate, i.DueDate = priority, startDate, dueDate
	return nil
}


func (i *Issue) IsAccessibleBy(u *User) bool {
	if u == nil {
		return false
	}

	if i.Author != nil && i.Author.ID == u.ID {
		return true
	}

	return i.Assignee != nil && i.Assignee.ID == u.ID
}

func normaliseText(name string, description *string, problems map[string]string) (string, *string) {
	name = strings.TrimSpace(name)

	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		problems["name"] = "is required"
		
	case n > MaxNameLength:
		problems["name"] = "must be at most 200 characters"
	}

	var text *string
	if description != nil {
		if len(*description) > MaxDescriptionBytes {
			problems["description"] = "must be at most 1 MiB"
		} else if d := strings.TrimSpace(*description); d != "" {
			text = &d
		}
	}
	return name, text
}

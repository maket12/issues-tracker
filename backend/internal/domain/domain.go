package domain

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrUnauthorized    = errors.New("authentication required")
	ErrForbidden       = errors.New("access denied")
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("the issue was changed by someone else, reload it and try again")
	ErrNoInitialStatus = errors.New("no initial status is configured")

	// Errors of the issue lifecycle (LC-1 .. LC-5).
	ErrStatusExists         = errors.New("a status with this name already exists")
	ErrTransitionExists     = errors.New("this transition already exists")
	ErrTransitionNotAllowed = errors.New("this status transition is not allowed")

	ErrSprintClosed         = errors.New("sprint is closed")
	ErrIssueAlreadyInSprint = errors.New("issue is already in this sprint")
)

type ValidationError struct {
	Fields map[string]string
}

func NewValidation(field, msg string) *ValidationError {
	return &ValidationError{
		Fields: map[string]string{field: msg},
	}
}

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))

	for k := range e.Fields {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	parts := make([]string, 0, len(keys))

	for _, k := range keys {
		parts = append(parts, k+": "+e.Fields[k])
	}

	return "validation failed: " + strings.Join(parts, "; ")
}

type Permission string

const (
	CreateIssue        Permission = "CREATE_ISSUE"
	ViewIssue          Permission = "VIEW_ISSUE"
	EditIssue          Permission = "EDIT_ISSUE"
	DeleteIssue        Permission = "DELETE_ISSUE"
	ManageRoles        Permission = "MANAGE_ROLES"
	ConfigureLifecycle Permission = "CONFIGURE_LIFECYCLE"
	ManageSprints      Permission = "MANAGE_SPRINTS"
)

// Equals is `permission.equals(requiredPermission)` from the MA-4 sequence diagram.
func (p Permission) Equals(other Permission) bool {
	return p == other
}

type IssueType struct {
	ID          string
	Name        string
	Description *string
}

type Priority struct {
	ID          string
	Name        string
	Description *string
}

func (p *Priority) GetName() string {
	if p == nil {
		return ""
	}
	return p.Name
}

type Status struct {
	ID   string
	Name string
}

type Date struct {
	time.Time
}

const dateLayout = "2006-01-02"

func (d Date) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Format(dateLayout))
}

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string

	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("date must be a string in YYYY-MM-DD format")
	}

	t, err := time.Parse(dateLayout, s)

	if err != nil {
		return fmt.Errorf("date must be in YYYY-MM-DD format")
	}

	d.Time = t
	return nil
}

func (d *Date) Scan(src any) error {
	switch v := src.(type) {

	case time.Time:
		d.Time = v

	case []byte:
		t, err := time.Parse(dateLayout, string(v))
		if err != nil {
			return err
		}
		d.Time = t

	case string:
		t, err := time.Parse(dateLayout, v)
		if err != nil {
			return err
		}
		d.Time = t

	default:
		return fmt.Errorf("cannot scan %T into Date", src)

	}

	return nil
}

func (d Date) Value() (driver.Value, error) {
	return d.Format(dateLayout), nil
}

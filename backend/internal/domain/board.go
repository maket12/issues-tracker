package domain

import (
	"slices"
	"sort"
	"strings"
)

// Board represents a user's visual task board (WO-3 .. WO-5).
type Board struct {
	UserID string
	issues []*Issue
}

// NewBoard creates a board with the given accessible issues.
func NewBoard(userID string, issues []*Issue) *Board {
	return &Board{
		UserID: userID,
		issues: slices.Clone(issues),
	}
}

func (b *Board) GetIssues() []*Issue {
	if b == nil {
		return nil
	}
	return slices.Clone(b.issues)
}

// GetColumns groups the board's issues by their status name, preserving current issue order.
func (b *Board) GetColumns() map[string][]*Issue {
	cols := make(map[string][]*Issue)
	if b == nil {
		return cols
	}
	for _, issue := range b.issues {
		if issue == nil {
			continue
		}
		statusName := issue.GetStatus().GetName()
		cols[statusName] = append(cols[statusName], issue)
	}
	return cols
}

// ApplyFilter keeps only issues for which filter returns true (WO-4).
func (b *Board) ApplyFilter(filter func(*Issue) bool) []*Issue {
	if b == nil {
		return nil
	}
	if filter == nil {
		return b.GetIssues()
	}
	filtered := make([]*Issue, 0, len(b.issues))
	for _, issue := range b.issues {
		if issue != nil && filter(issue) {
			filtered = append(filtered, issue)
		}
	}
	b.issues = filtered
	return b.GetIssues()
}

// SortBy orders the board's issues by the given attribute and direction ("asc" or "desc") (WO-5).
// Empty/nil values are always placed at the end regardless of direction.
func (b *Board) SortBy(attribute, direction string) ([]*Issue, error) {
	if b == nil {
		return nil, nil
	}
	attr := strings.ToLower(strings.TrimSpace(attribute))
	dir := strings.ToLower(strings.TrimSpace(direction))
	if dir == "" {
		dir = "asc"
	}

	problems := map[string]string{}
	switch attr {
	case "priority", "startdate", "duedate", "name":
	default:
		problems["attribute"] = "must be one of: priority, startDate, dueDate, name"
	}
	if dir != "asc" && dir != "desc" {
		problems["direction"] = "must be asc or desc"
	}
	if len(problems) > 0 {
		return nil, &ValidationError{Fields: problems}
	}

	desc := dir == "desc"
	sort.SliceStable(b.issues, func(i, j int) bool {
		a, c := b.issues[i], b.issues[j]
		if a == nil && c == nil {
			return false
		}
		if a == nil {
			return false
		}
		if c == nil {
			return true
		}

		switch attr {
		case "priority":
			pa, pc := a.GetPriority(), c.GetPriority()
			aEmpty := pa == nil || strings.TrimSpace(pa.GetName()) == ""
			cEmpty := pc == nil || strings.TrimSpace(pc.GetName()) == ""
			if aEmpty || cEmpty {
				return !aEmpty && cEmpty
			}
			cmp := comparePriority(pa.GetName(), pc.GetName())
			if desc {
				return cmp > 0
			}
			return cmp < 0

		case "startdate":
			da, dc := a.GetStartDate(), c.GetStartDate()
			aEmpty, cEmpty := da.IsZero(), dc.IsZero()
			if aEmpty || cEmpty {
				return !aEmpty && cEmpty
			}
			if desc {
				return da.After(dc.Time)
			}
			return da.Before(dc.Time)

		case "duedate":
			da, dc := a.GetDueDate(), c.GetDueDate()
			aEmpty := da == nil || da.IsZero()
			cEmpty := dc == nil || dc.IsZero()
			if aEmpty || cEmpty {
				return !aEmpty && cEmpty
			}
			if desc {
				return da.After(dc.Time)
			}
			return da.Before(dc.Time)

		case "name":
			na, nc := strings.ToLower(strings.TrimSpace(a.Name)), strings.ToLower(strings.TrimSpace(c.Name))
			aEmpty, cEmpty := na == "", nc == ""
			if aEmpty || cEmpty {
				return !aEmpty && cEmpty
			}
			if desc {
				return na > nc
			}
			return na < nc
		}
		return false
	})

	return b.GetIssues(), nil
}

func comparePriority(a, b string) int {
	wa, okA := priorityWeight(a)
	wb, okB := priorityWeight(b)
	if okA && okB {
		switch {
		case wa < wb:
			return -1
		case wa > wb:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b)))
}

func priorityWeight(name string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "low":
		return 1, true
	case "medium":
		return 2, true
	case "high":
		return 3, true
	case "critical":
		return 4, true
	default:
		return 0, false
	}
}

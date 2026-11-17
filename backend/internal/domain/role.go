package domain

import "slices"

type Role struct {
	ID          string
	Name        string
	Permissions []Permission
}

func (r *Role) HasPermission(p Permission) bool {
	return r != nil && slices.Contains(r.Permissions, p)
}

// GetPermissions returns a copy of the role's permissions.
func (r *Role) GetPermissions() []Permission {
	if r == nil {
		return nil
	}
	return slices.Clone(r.Permissions)
}

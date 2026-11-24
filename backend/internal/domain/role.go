package domain

import "slices"

type Role struct {
	ID          string
	Name        string
	Permissions []Permission
}

// HasPermission is `role.hasPermission(p)`: it asks every granted permission whether it equals p.
func (r *Role) HasPermission(p Permission) bool {
	if r == nil {
		return false
	}
	for _, granted := range r.Permissions {
		if granted.Equals(p) {
			return true
		}
	}
	return false
}

// GetPermissions returns a copy of the role's permissions.
func (r *Role) GetPermissions() []Permission {
	if r == nil {
		return nil
	}
	return slices.Clone(r.Permissions)
}

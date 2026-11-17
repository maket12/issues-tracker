package domain

// Session represents an authenticated user session opened by MA-2 (Log in).
type Session struct {
	ID   string
	User *User
	Role *Role
}

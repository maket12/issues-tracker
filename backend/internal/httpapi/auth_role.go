package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

type AuthUseCases interface {
	Register(ctx context.Context, name, email, password string) (*domain.User, error)
	Login(ctx context.Context, email, password string) (*domain.Session, error)
	Logout(ctx context.Context, user *domain.User) (bool, error)
}

type RoleUseCases interface {
	AssignRole(ctx context.Context, admin *domain.User, targetUserID, newRoleID string) (*domain.User, error)
}

type SessionLookup interface {
	FindByID(ctx context.Context, sessionID string) (*domain.Session, error)
}

type UserLookup interface {
	FindByID(ctx context.Context, id string) (*domain.User, error)
}

// SessionAuthenticator resolves the authenticated user from `Authorization: Bearer <token>` or the `session_id` cookie.
func SessionAuthenticator(sessions SessionLookup, users UserLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token != "" {
				if sess, err := sessions.FindByID(r.Context(), token); err == nil && sess.User != nil {
					if freshUser, err := users.FindByID(r.Context(), sess.User.GetID()); err == nil {
						r = r.WithContext(WithUser(r.Context(), freshUser))
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	if c, err := r.Cookie("session_id"); err == nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type assignRoleRequest struct {
	RoleID string `json:"roleId"`
}

type userDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  refDTO `json:"role"`
}

type sessionDTO struct {
	Token string  `json:"token"`
	User  userDTO `json:"user"`
}

func toUserDTO(u *domain.User) userDTO {
	var role refDTO
	if r := u.GetRole(); r != nil {
		role = refDTO{ID: r.ID, Name: r.Name}
	}
	return userDTO{
		ID:    u.ID,
		Name:  u.Name,
		Email: u.Email,
		Role:  role,
	}
}

// register handles POST /api/auth/register (MA-1).
func register(uc AuthUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		if !decodeBody(w, r, &req) {
			return
		}
		user, err := uc.Register(r.Context(), req.Name, req.Email, req.Password)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toUserDTO(user))
	}
}

// login handles POST /api/auth/login (MA-2).
func login(uc AuthUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if !decodeBody(w, r, &req) {
			return
		}
		sess, err := uc.Login(r.Context(), req.Email, req.Password)
		if err != nil {
			writeError(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "session_id",
			Value:    sess.ID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		writeJSON(w, http.StatusOK, sessionDTO{
			Token: sess.ID,
			User:  toUserDTO(sess.User),
		})
	}
}

// logout handles POST /api/auth/logout (MA-2).
func logout(uc AuthUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		closed, err := uc.Logout(r.Context(), user)
		if err != nil {
			writeError(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "session_id",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
		})
		writeJSON(w, http.StatusOK, map[string]bool{"loggedOut": closed})
	}
}

// assignRole handles PUT /api/users/{id}/role (MA-3).
func assignRole(uc RoleUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin := UserFrom(r.Context())
		if admin == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		var req assignRoleRequest
		if !decodeBody(w, r, &req) {
			return
		}
		updated, err := uc.AssignRole(r.Context(), admin, chi.URLParam(r, "id"), req.RoleID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toUserDTO(updated))
	}
}

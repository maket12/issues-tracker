package httpapi

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-sql-driver/mysql"

	"github.com/maket12/issues-tracker/backend/internal/domain"
	"github.com/maket12/issues-tracker/backend/internal/service"
)

const maxBodyBytes = 2 << 20

type IssueUseCases interface {
	CreateIssue(ctx context.Context, name, typeID string, description *string, author *domain.User) (*domain.Issue, error)
	FillAttributes(ctx context.Context, issueID string, attr service.AttributeData, user *domain.User) (*domain.Issue, error)
	ViewIssue(ctx context.Context, issueID string, user *domain.User) (*domain.Issue, error)
	EditIssue(ctx context.Context, issueID string, changes service.AttributeData, user *domain.User) (*domain.Issue, error)
}

type userKey struct{}

func WithUser(ctx context.Context, u *domain.User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

func UserFrom(ctx context.Context) *domain.User {
	u, _ := ctx.Value(userKey{}).(*domain.User)
	return u
}

func NewRouter(issues IssueUseCases, authenticate func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Route("/api", func(r chi.Router) {
		r.Use(authenticate)
		r.Post("/issues", createIssue(issues))
		r.Get("/issues/{id}", viewIssue(issues))
		r.Put("/issues/{id}", fillAttributes(issues))
		r.Patch("/issues/{id}", editIssue(issues))
	})
	return r
}

type createIssueRequest struct {
	Name        string  `json:"name"`
	TypeID      string  `json:"typeId"`
	Description *string `json:"description"`
}

type fillAttributesRequest struct {
	Name        string       `json:"name"`
	TypeID      string       `json:"typeId"`
	Description *string      `json:"description"`
	AssigneeID  *string      `json:"assigneeId"`
	PriorityID  string       `json:"priorityId"`
	StartDate   *domain.Date `json:"startDate"`
	DueDate     *domain.Date `json:"dueDate"`
}

type editIssueRequest struct {
	Name        string       `json:"name"`
	TypeID      string       `json:"typeId"`
	Description *string      `json:"description"`
	AssigneeID  *string      `json:"assigneeId"`
	PriorityID  string       `json:"priorityId"`
	StartDate   *domain.Date `json:"startDate"`
	DueDate     *domain.Date `json:"dueDate"`
	Version     string       `json:"version"`
}

type refDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type issueDTO struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description *string      `json:"description"`
	Type        refDTO       `json:"type"`
	Author      refDTO       `json:"author"`
	Assignee    *refDTO      `json:"assignee"`
	Status      refDTO       `json:"status"`
	Priority    refDTO       `json:"priority"`
	StartDate   domain.Date  `json:"startDate"`
	DueDate     *domain.Date `json:"dueDate"`
	Version     string       `json:"version"`
}

func toDTO(i *domain.Issue) issueDTO {
	d := issueDTO{
		ID: i.ID, Name: i.Name, Description: i.Description,
		Type:      refDTO{ID: i.Type.ID, Name: i.Type.Name},
		Author:    refDTO{ID: i.Author.ID, Name: i.Author.Name},
		Status:    refDTO{ID: i.Status.ID, Name: i.Status.Name},
		Priority:  refDTO{ID: i.Priority.ID, Name: i.Priority.Name},
		StartDate: i.StartDate, DueDate: i.DueDate, Version: i.Version,
	}
	if i.Assignee != nil {
		d.Assignee = &refDTO{ID: i.Assignee.ID, Name: i.Assignee.Name}
	}
	return d
}

func createIssue(uc IssueUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		var req createIssueRequest
		if !decodeBody(w, r, &req) {
			return
		}
		issue, err := uc.CreateIssue(r.Context(), req.Name, req.TypeID, req.Description, user)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Location", "/api/issues/"+issue.ID)
		writeJSON(w, http.StatusCreated, toDTO(issue))
	}
}

func fillAttributes(uc IssueUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		var req fillAttributesRequest
		if !decodeBody(w, r, &req) {
			return
		}
		issue, err := uc.FillAttributes(r.Context(), chi.URLParam(r, "id"), service.AttributeData{
			Name: req.Name, TypeID: req.TypeID, Description: req.Description, AssigneeID: req.AssigneeID,
			PriorityID: req.PriorityID, StartDate: req.StartDate, DueDate: req.DueDate,
		}, user)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toDTO(issue))
	}
}

func viewIssue(uc IssueUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		issue, err := uc.ViewIssue(r.Context(), chi.URLParam(r, "id"), user)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toDTO(issue))
	}
}

func editIssue(uc IssueUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		var req editIssueRequest
		if !decodeBody(w, r, &req) {
			return
		}
		issue, err := uc.EditIssue(r.Context(), chi.URLParam(r, "id"), service.AttributeData{
			Name: req.Name, TypeID: req.TypeID, Description: req.Description, AssigneeID: req.AssigneeID,
			PriorityID: req.PriorityID, StartDate: req.StartDate, DueDate: req.DueDate, Version: req.Version,
		}, user)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toDTO(issue))
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body: " + err.Error()})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	var ve *domain.ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "validation failed", "fields": ve.Fields})
	case errors.Is(err, domain.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
	case errors.Is(err, domain.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
	case errors.Is(err, domain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case errors.Is(err, domain.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrNoInitialStatus):
		log.Printf("misconfiguration: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no initial status is configured"})
	case isDBUnavailable(err):
		log.Printf("database unavailable: %v", err)
		w.Header().Set("Retry-After", "5")
		writeJSON(w, http.StatusServiceUnavailable,
			map[string]string{"error": "database is temporarily unavailable, please retry shortly"})
	default:
		log.Printf("internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
	}
}

func isDBUnavailable(err error) bool {
	var ne net.Error
	return errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, mysql.ErrInvalidConn) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &ne)
}
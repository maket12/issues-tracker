package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

type SprintUseCases interface {
	CreateSprint(ctx context.Context, name string, startDate, endDate domain.Date, user *domain.User) (*domain.Sprint, error)
	LinkIssueToSprint(ctx context.Context, sprintID, issueID string, user *domain.User) (*domain.Sprint, error)
}

type BoardUseCases interface {
	ViewBoard(ctx context.Context, user *domain.User) (map[string][]*domain.Issue, error)
	FilterBoard(ctx context.Context, assigneeID, sprintID string, user *domain.User) (map[string][]*domain.Issue, error)
	SortBoard(ctx context.Context, attribute, direction string, user *domain.User) (map[string][]*domain.Issue, error)
}

type createSprintRequest struct {
	Name      string      `json:"name"`
	StartDate domain.Date `json:"startDate"`
	EndDate   domain.Date `json:"endDate"`
}

type linkIssueRequest struct {
	IssueID string `json:"issueId"`
}

type sprintDTO struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	StartDate domain.Date  `json:"startDate"`
	EndDate   *domain.Date `json:"endDate"`
	Closed    bool         `json:"closed"`
	Issues    []issueDTO   `json:"issues"`
}

func toSprintDTO(s *domain.Sprint) sprintDTO {
	issues := s.GetIssues()
	dtos := make([]issueDTO, 0, len(issues))
	for _, iss := range issues {
		dtos = append(dtos, toDTO(iss))
	}
	return sprintDTO{
		ID:        s.ID,
		Name:      s.Name(),
		StartDate: s.StartDate(),
		EndDate:   s.EndDate,
		Closed:    s.IsClosed(),
		Issues:    dtos,
	}
}

type boardDTO struct {
	Columns map[string][]issueDTO `json:"columns"`
}

func toBoardDTO(cols map[string][]*domain.Issue) boardDTO {
	out := make(map[string][]issueDTO, len(cols))
	for status, list := range cols {
		dtos := make([]issueDTO, 0, len(list))
		for _, iss := range list {
			dtos = append(dtos, toDTO(iss))
		}
		out[status] = dtos
	}
	return boardDTO{Columns: out}
}

// createSprint handles POST /api/sprints (WO-1).
func createSprint(uc SprintUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		var req createSprintRequest
		if !decodeBody(w, r, &req) {
			return
		}
		sprint, err := uc.CreateSprint(r.Context(), req.Name, req.StartDate, req.EndDate, user)
		if err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Location", "/api/sprints/"+sprint.ID)
		writeJSON(w, http.StatusCreated, toSprintDTO(sprint))
	}
}

// linkIssueToSprint handles POST /api/sprints/{id}/issues (WO-2).
func linkIssueToSprint(uc SprintUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		var req linkIssueRequest
		if !decodeBody(w, r, &req) {
			return
		}
		sprint, err := uc.LinkIssueToSprint(r.Context(), chi.URLParam(r, "id"), req.IssueID, user)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toSprintDTO(sprint))
	}
}

// viewBoard handles GET /api/board (WO-3).
func viewBoard(uc BoardUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		cols, err := uc.ViewBoard(r.Context(), user)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toBoardDTO(cols))
	}
}

// filterBoard handles GET /api/board/filter?assigneeId=...&sprintId=... (WO-4).
func filterBoard(uc BoardUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		q := r.URL.Query()
		cols, err := uc.FilterBoard(r.Context(), q.Get("assigneeId"), q.Get("sprintId"), user)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toBoardDTO(cols))
	}
}

// sortBoard handles GET /api/board/sort?attribute=...&direction=... (WO-5).
func sortBoard(uc BoardUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		q := r.URL.Query()
		cols, err := uc.SortBoard(r.Context(), q.Get("attribute"), q.Get("direction"), user)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toBoardDTO(cols))
	}
}

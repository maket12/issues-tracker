package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// LifecycleUseCases is the part of service.LifecycleService the handlers need.
type LifecycleUseCases interface {
	DefineStatus(ctx context.Context, statusName string, user *domain.User) (*domain.Status, error)
	DefineTransition(ctx context.Context, fromID, toID string, user *domain.User) (*domain.TransitionRule, error)
	SetTransitionRulesByID(ctx context.Context, fromID, toID, roleID string, allowAuthor bool, user *domain.User) (*domain.TransitionRule, error)
}

type defineStatusRequest struct {
	Name string `json:"name"`
}

type defineTransitionRequest struct {
	FromID string `json:"fromId"`
	ToID   string `json:"toId"`
}

type setTransitionRulesRequest struct {
	RoleID      string `json:"roleId"`
	AllowAuthor bool   `json:"allowAuthor"`
}

type transitionDTO struct {
	From         refDTO   `json:"from"`
	To           refDTO   `json:"to"`
	AllowAuthor  bool     `json:"allowAuthor"`
	AllowedRoles []refDTO `json:"allowedRoles"`
}

func toTransitionDTO(rule *domain.TransitionRule) transitionDTO {
	d := transitionDTO{
		From:         refDTO{ID: rule.From.ID, Name: rule.From.Name},
		To:           refDTO{ID: rule.To.ID, Name: rule.To.Name},
		AllowAuthor:  rule.AllowsAuthor(),
		AllowedRoles: []refDTO{},
	}
	for _, role := range rule.GetAllowedRoles() {
		d.AllowedRoles = append(d.AllowedRoles, refDTO{ID: role.ID, Name: role.Name})
	}
	return d
}

// defineStatus handles POST /api/statuses (LC-1).
//
//	401 no authenticated user    403 no CONFIGURE_LIFECYCLE right    422 empty name
//	409 the name is already taken    201 created, body = the new status
func defineStatus(uc LifecycleUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		var req defineStatusRequest
		if !decodeBody(w, r, &req) {
			return
		}
		status, err := uc.DefineStatus(r.Context(), req.Name, user)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, refDTO{ID: status.ID, Name: status.Name})
	}
}

// defineTransition handles POST /api/transitions (LC-2).
//
//	401 no authenticated user    403 no CONFIGURE_LIFECYCLE right
//	422 unknown status, or from == to    409 the transition already exists
//	201 created, body = the transition (nobody may use it until LC-3 grants a role or the author)
func defineTransition(uc LifecycleUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		var req defineTransitionRequest
		if !decodeBody(w, r, &req) {
			return
		}
		rule, err := uc.DefineTransition(r.Context(), req.FromID, req.ToID, user)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toTransitionDTO(rule))
	}
}

// setTransitionRules handles PUT /api/transitions/{fromId}/{toId} (LC-3): adds a role to the transition
// and sets whether the author may use it.
//
//	401 no authenticated user    403 no CONFIGURE_LIFECYCLE right
//	404 the transition is not defined    422 unknown status or role
//	200 stored, body = the transition
func setTransitionRules(uc LifecycleUseCases) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			writeError(w, domain.ErrUnauthorized)
			return
		}
		var req setTransitionRulesRequest
		if !decodeBody(w, r, &req) {
			return
		}
		rule, err := uc.SetTransitionRulesByID(r.Context(), chi.URLParam(r, "fromId"), chi.URLParam(r, "toId"),
			req.RoleID, req.AllowAuthor, user)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toTransitionDTO(rule))
	}
}

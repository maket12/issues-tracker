package repository

import (
	"context"
	"sync"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// SessionRepo stores active user sessions in memory (MA-2).
type SessionRepo struct {
	mu       sync.RWMutex
	byID     map[string]*domain.Session
	byUserID map[string]map[string]struct{}
}

func NewSessionRepo() *SessionRepo {
	return &SessionRepo{
		byID:     make(map[string]*domain.Session),
		byUserID: make(map[string]map[string]struct{}),
	}
}

func (r *SessionRepo) SaveSession(_ context.Context, user *domain.User, role *domain.Role) (*domain.Session, error) {
	if user == nil || user.GetID() == "" {
		return nil, domain.ErrUnauthorized
	}
	token, err := randomHex(24)
	if err != nil {
		return nil, err
	}

	sess := &domain.Session{
		ID:   token,
		User: user,
		Role: role,
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.byID[token] = sess
	if r.byUserID[user.GetID()] == nil {
		r.byUserID[user.GetID()] = make(map[string]struct{})
	}
	r.byUserID[user.GetID()][token] = struct{}{}
	return sess, nil
}

func (r *SessionRepo) FindByID(_ context.Context, sessionID string) (*domain.Session, error) {
	if sessionID == "" {
		return nil, domain.ErrNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	sess, ok := r.byID[sessionID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return sess, nil
}

func (r *SessionRepo) DeleteByUserID(_ context.Context, userID string) (bool, error) {
	if userID == "" {
		return false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	tokens, ok := r.byUserID[userID]
	if !ok || len(tokens) == 0 {
		return false, nil
	}
	for token := range tokens {
		delete(r.byID, token)
	}
	delete(r.byUserID, userID)
	return true, nil
}

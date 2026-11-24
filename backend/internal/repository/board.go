package repository

import (
	"context"
	"sync"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

type BoardRepo struct {
	mu     sync.RWMutex
	boards map[string]*domain.Board
}

func NewBoardRepo() *BoardRepo {
	return &BoardRepo{boards: make(map[string]*domain.Board)}
}

func (r *BoardRepo) FindByUser(_ context.Context, user *domain.User) (*domain.Board, error) {
	if user == nil || user.GetID() == "" {
		return nil, domain.ErrNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	board, ok := r.boards[user.GetID()]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return domain.NewBoard(board.UserID, board.GetIssues()), nil
}

func (r *BoardRepo) Save(_ context.Context, board *domain.Board) (*domain.Board, error) {
	if board == nil || board.UserID == "" {
		return nil, domain.NewValidation("userId", "is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	stored := domain.NewBoard(board.UserID, board.GetIssues())
	r.boards[board.UserID] = stored
	return domain.NewBoard(stored.UserID, stored.GetIssues()), nil
}

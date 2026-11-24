package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// BoardRepository persists each user's current board state (WO-3 .. WO-5).
type BoardRepository interface {
	FindByUser(ctx context.Context, user *domain.User) (*domain.Board, error)
	Save(ctx context.Context, board *domain.Board) (*domain.Board, error)
}

// BoardService implements WO-3 (View board), WO-4 (Filter board) and WO-5 (Sort board).
type BoardService struct {
	boards  BoardRepository
	issues  IssueRepository
	sprints SprintRepository
}

func NewBoardService(boards BoardRepository, issues IssueRepository, sprints SprintRepository) *BoardService {
	return &BoardService{boards: boards, issues: issues, sprints: sprints}
}

// ViewBoard implements WO-3: viewBoard(user).
func (s *BoardService) ViewBoard(ctx context.Context, user *domain.User) (map[string][]*domain.Issue, error) {
	if user == nil {
		return nil, domain.ErrUnauthorized
	}

	// IssueRepository.findAll() -> allIssues
	allIssues, err := s.issues.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("find all issues: %w", err)
	}

	// loop [for each issue in allIssues]: isAccessibleBy(user), getStatus().getName()
	accessible := make([]*domain.Issue, 0, len(allIssues))
	for _, issue := range allIssues {
		if issue == nil || !issue.IsAccessibleBy(user) {
			continue
		}
		_ = issue.GetStatus().GetName()
		accessible = append(accessible, issue)
	}

	// new Board(accessibleIssues), board.getColumns() -> columnsByStatus
	board := domain.NewBoard(user.GetID(), accessible)
	columns := board.GetColumns()

	// BoardRepository.save(board) -> savedBoard
	if _, err := s.boards.Save(ctx, board); err != nil {
		return nil, fmt.Errorf("save board: %w", err)
	}
	return columns, nil
}

// FilterBoard implements WO-4: filterBoard(assigneeId, sprintId, user).
func (s *BoardService) FilterBoard(ctx context.Context, assigneeID, sprintID string, user *domain.User) (map[string][]*domain.Issue, error) {
	if user == nil {
		return nil, domain.ErrUnauthorized
	}

	// BoardRepository.findByUser(user) -> board
	board, err := s.boards.FindByUser(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("find board: %w", err)
	}

	assigneeID = strings.TrimSpace(assigneeID)
	sprintID = strings.TrimSpace(sprintID)

	// SprintRepository.findById(sprintId) -> sprint (when sprint filter is selected)
	var sprint *domain.Sprint
	if sprintID != "" {
		sprint, err = s.sprints.FindByID(ctx, sprintID)
		if err != nil {
			return nil, fmt.Errorf("find sprint: %w", err)
		}
	}

	// board.applyFilter(filterFunc) -> filteredIssues
	board.ApplyFilter(func(issue *domain.Issue) bool {
		if assigneeID != "" {
			assignee := issue.GetAssignee()
			if assignee == nil || assignee.GetID() != assigneeID {
				return false
			}
		}
		if sprint != nil && !sprint.Contains(issue) {
			return false
		}
		return true
	})

	// board.getColumns() -> filteredColumns
	columns := board.GetColumns()

	// BoardRepository.save(board) -> updatedBoard
	if _, err := s.boards.Save(ctx, board); err != nil {
		return nil, fmt.Errorf("save board: %w", err)
	}
	return columns, nil
}

// SortBoard implements WO-5: sortBoard(attribute, direction, user).
func (s *BoardService) SortBoard(ctx context.Context, attribute, direction string, user *domain.User) (map[string][]*domain.Issue, error) {
	if user == nil {
		return nil, domain.ErrUnauthorized
	}

	// BoardRepository.findByUser(user) -> board
	board, err := s.boards.FindByUser(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("find board: %w", err)
	}

	// board.sortBy(attribute, direction) -> sortedIssues
	if _, err := board.SortBy(attribute, direction); err != nil {
		return nil, err
	}

	// board.getColumns() -> sortedColumns
	columns := board.GetColumns()

	// IssueRepository.findAllSorted(attribute, direction) -> persistedOrder
	if _, err := s.issues.FindAllSorted(ctx, attribute, direction); err != nil {
		return nil, fmt.Errorf("find all sorted issues: %w", err)
	}

	// BoardRepository.save(board) -> updatedBoard
	if _, err := s.boards.Save(ctx, board); err != nil {
		return nil, fmt.Errorf("save board: %w", err)
	}
	return columns, nil
}

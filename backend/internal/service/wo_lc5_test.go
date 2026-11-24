// Tests for LC-5 (Verify transition right) and WO-1 .. WO-5 (Sprints & Board).
package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maket12/issues-tracker/backend/internal/domain"
	"github.com/maket12/issues-tracker/backend/internal/service"
)

// ================================================================ mocks

type woIssueRepo struct {
	log    *callLog
	byID   map[string]*domain.Issue
	all    []*domain.Issue
	sorted []*domain.Issue
}

func (m *woIssueRepo) FindByID(_ context.Context, id string) (*domain.Issue, error) {
	m.log.add("IssueRepository.FindByID(%s)", id)
	i, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return i, nil
}
func (m *woIssueRepo) FindAll(context.Context) ([]*domain.Issue, error) {
	m.log.add("IssueRepository.FindAll")
	return m.all, nil
}
func (m *woIssueRepo) FindAllSorted(_ context.Context, attr, dir string) ([]*domain.Issue, error) {
	m.log.add("IssueRepository.FindAllSorted(%s, %s)", attr, dir)
	return m.sorted, nil
}
func (m *woIssueRepo) FindTypeByID(context.Context, string) (*domain.IssueType, error) {
	return nil, domain.ErrNotFound
}
func (m *woIssueRepo) FindPriorityByID(context.Context, string) (*domain.Priority, error) {
	return nil, domain.ErrNotFound
}
func (m *woIssueRepo) FindUserByID(context.Context, string) (*domain.User, error) {
	return nil, domain.ErrNotFound
}
func (m *woIssueRepo) FindInitialStatus(context.Context) (*domain.Status, error) {
	return nil, domain.ErrNotFound
}
func (m *woIssueRepo) FindStatusByID(context.Context, string) (*domain.Status, error) {
	return nil, domain.ErrNotFound
}
func (m *woIssueRepo) Save(_ context.Context, i *domain.Issue) (*domain.Issue, error) {
	return i, nil
}
func (m *woIssueRepo) Delete(context.Context, *domain.Issue) (bool, error) {
	return false, nil
}

type woSprintRepo struct {
	log   *callLog
	byID  map[string]*domain.Sprint
	saved *domain.Sprint
}

func (m *woSprintRepo) FindByID(_ context.Context, id string) (*domain.Sprint, error) {
	m.log.add("SprintRepository.FindByID(%s)", id)
	s, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return s, nil
}
func (m *woSprintRepo) FindByIssue(context.Context, *domain.Issue) (*domain.Sprint, error) {
	return nil, domain.ErrNotFound
}
func (m *woSprintRepo) Save(_ context.Context, s *domain.Sprint) (*domain.Sprint, error) {
	m.log.add("SprintRepository.Save(%s)", s.Name())
	if s.ID == "" {
		s.ID = "SPR-1"
	}
	m.saved = s
	return s, nil
}

type woBoardRepo struct {
	log    *callLog
	boards map[string]*domain.Board
	saved  *domain.Board
}

func (m *woBoardRepo) FindByUser(_ context.Context, u *domain.User) (*domain.Board, error) {
	m.log.add("BoardRepository.FindByUser(%s)", u.GetID())
	b, ok := m.boards[u.GetID()]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return b, nil
}
func (m *woBoardRepo) Save(_ context.Context, b *domain.Board) (*domain.Board, error) {
	m.log.add("BoardRepository.Save(%s)", b.UserID)
	m.saved = b
	m.boards[b.UserID] = b
	return b, nil
}

// ================================================================ LC-5 VerifyTransition

func TestVerifyTransition_FollowsSequenceDiagram(t *testing.T) {
	log := &callLog{}
	open, done := &domain.Status{ID: "open", Name: "Open"}, &domain.Status{ID: "done", Name: "Done"}
	rule, _ := domain.NewTransitionRule(open, done)
	rule.AddRole(&domain.Role{ID: "qa"})
	rule.SetAllowAuthor(true)

	lcRepo := &lcLifecycleRepo{
		log:       log,
		lifecycle: domain.RestoreLifecycle([]*domain.Status{open, done}, []*domain.TransitionRule{rule}),
	}
	svc := service.NewLifecycleService(lcRepo, nil, nil)

	alice := &domain.User{ID: "alice", Role: &domain.Role{ID: "member"}}
	quinn := &domain.User{ID: "quinn", Role: &domain.Role{ID: "qa"}}
	carol := &domain.User{ID: "carol", Role: &domain.Role{ID: "member"}}
	issue := &domain.Issue{ID: "ISS-1", Author: alice, Status: open}

	ok, err := svc.VerifyTransition(ctx, issue, alice, done)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = svc.VerifyTransition(ctx, issue, quinn, done)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = svc.VerifyTransition(ctx, issue, carol, done)
	assert.False(t, ok)
	assert.ErrorIs(t, err, domain.ErrForbidden)
}

// ================================================================ WO-1 CreateSprint

func TestCreateSprint_FollowsSequenceDiagram(t *testing.T) {
	log := &callLog{}
	sprints := &woSprintRepo{log: log, byID: map[string]*domain.Sprint{}}
	svc := service.NewSprintService(sprints, nil)
	mgr := userWith("dana", domain.ManageSprints)

	saved, err := svc.CreateSprint(ctx, "Sprint 2", day("2026-10-26"), day("2026-11-09"), mgr)

	require.NoError(t, err)
	assert.Equal(t, "SPR-1", saved.ID)
	assert.Equal(t, "Sprint 2", saved.Name())
	assert.Empty(t, saved.GetIssues())
	assert.Equal(t, []string{"SprintRepository.Save(Sprint 2)"}, log.entries)
}

func TestCreateSprint_Rejections(t *testing.T) {
	log := &callLog{}
	svc := service.NewSprintService(&woSprintRepo{log: log}, nil)
	mgr := userWith("dana", domain.ManageSprints)

	_, err := svc.CreateSprint(ctx, "S", day("2026-10-26"), day("2026-11-09"), nil)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)

	_, err = svc.CreateSprint(ctx, "S", day("2026-10-26"), day("2026-11-09"), userWith("alice", domain.CreateIssue))
	assert.ErrorIs(t, err, domain.ErrForbidden)

	_, err = svc.CreateSprint(ctx, "   ", day("2026-10-26"), day("2026-11-09"), mgr)
	assert.Contains(t, fieldsOf(t, err), "name")

	_, err = svc.CreateSprint(ctx, "Sprint", day("2026-11-10"), day("2026-11-01"), mgr)
	assert.Contains(t, fieldsOf(t, err), "endDate")
	assert.Empty(t, log.entries)
}

// ================================================================ WO-2 LinkIssueToSprint

func TestLinkIssueToSprint_FollowsSequenceDiagram(t *testing.T) {
	log := &callLog{}
	sprint := domain.RestoreSprint("SPR-1", "Sprint 1", day("2026-10-26"), nil, false, nil)
	issue := &domain.Issue{ID: "ISS-1", Name: "Task"}

	sprints := &woSprintRepo{log: log, byID: map[string]*domain.Sprint{"SPR-1": sprint}}
	issues := &woIssueRepo{log: log, byID: map[string]*domain.Issue{"ISS-1": issue}}
	svc := service.NewSprintService(sprints, issues)

	updated, err := svc.LinkIssueToSprint(ctx, "SPR-1", "ISS-1", userWith("dana", domain.ManageSprints))

	require.NoError(t, err)
	assert.True(t, updated.Contains(issue))
	assert.Equal(t, []string{
		"SprintRepository.FindByID(SPR-1)",
		"IssueRepository.FindByID(ISS-1)",
		"SprintRepository.Save(Sprint 1)",
	}, log.entries)
}

func TestLinkIssueToSprint_RejectsDuplicateOrClosed(t *testing.T) {
	mgr := userWith("dana", domain.ManageSprints)
	issue := &domain.Issue{ID: "ISS-1"}

	t.Run("already in sprint", func(t *testing.T) {
		log := &callLog{}
		sprint := domain.RestoreSprint("SPR-1", "S1", day("2026-10-26"), nil, false, []*domain.Issue{issue})
		svc := service.NewSprintService(
			&woSprintRepo{log: log, byID: map[string]*domain.Sprint{"SPR-1": sprint}},
			&woIssueRepo{log: log, byID: map[string]*domain.Issue{"ISS-1": issue}},
		)
		_, err := svc.LinkIssueToSprint(ctx, "SPR-1", "ISS-1", mgr)
		assert.ErrorIs(t, err, domain.ErrIssueAlreadyInSprint)
	})

	t.Run("sprint is closed", func(t *testing.T) {
		log := &callLog{}
		sprint := domain.RestoreSprint("SPR-1", "S1", day("2026-10-26"), nil, true, nil)
		svc := service.NewSprintService(
			&woSprintRepo{log: log, byID: map[string]*domain.Sprint{"SPR-1": sprint}},
			&woIssueRepo{log: log, byID: map[string]*domain.Issue{"ISS-1": issue}},
		)
		_, err := svc.LinkIssueToSprint(ctx, "SPR-1", "ISS-1", mgr)
		assert.ErrorIs(t, err, domain.ErrSprintClosed)
	})
}

// ================================================================ WO-3, WO-4, WO-5 Board

func TestViewBoard_FollowsSequenceDiagram(t *testing.T) {
	log := &callLog{}
	alice, bob, carol := &domain.User{ID: "alice"}, &domain.User{ID: "bob"}, &domain.User{ID: "carol"}
	open, done := &domain.Status{ID: "open", Name: "Open"}, &domain.Status{ID: "done", Name: "Done"}

	i1 := &domain.Issue{ID: "ISS-1", Author: alice, Status: open}
	i2 := &domain.Issue{ID: "ISS-2", Author: bob, Assignee: alice, Status: done}
	i3 := &domain.Issue{ID: "ISS-3", Author: bob, Assignee: carol, Status: open} // not accessible by alice

	issues := &woIssueRepo{log: log, all: []*domain.Issue{i1, i2, i3}}
	boards := &woBoardRepo{log: log, boards: map[string]*domain.Board{}}
	svc := service.NewBoardService(boards, issues, nil)

	cols, err := svc.ViewBoard(ctx, alice)

	require.NoError(t, err)
	assert.Len(t, cols["Open"], 1)
	assert.Equal(t, "ISS-1", cols["Open"][0].ID)
	assert.Len(t, cols["Done"], 1)
	assert.Equal(t, "ISS-2", cols["Done"][0].ID)
	assert.Equal(t, []string{
		"IssueRepository.FindAll",
		"BoardRepository.Save(alice)",
	}, log.entries)
}

func TestFilterBoard_FollowsSequenceDiagram(t *testing.T) {
	log := &callLog{}
	alice, bob := &domain.User{ID: "alice"}, &domain.User{ID: "bob"}
	open := &domain.Status{ID: "open", Name: "Open"}

	i1 := &domain.Issue{ID: "ISS-1", Author: alice, Assignee: bob, Status: open}
	i2 := &domain.Issue{ID: "ISS-2", Author: alice, Assignee: alice, Status: open}
	i3 := &domain.Issue{ID: "ISS-3", Author: alice, Assignee: bob, Status: open}

	board := domain.NewBoard("alice", []*domain.Issue{i1, i2, i3})
	sprint := domain.RestoreSprint("SPR-1", "Sprint 1", day("2026-10-26"), nil, false, []*domain.Issue{i1, i2})

	boards := &woBoardRepo{log: log, boards: map[string]*domain.Board{"alice": board}}
	sprints := &woSprintRepo{log: log, byID: map[string]*domain.Sprint{"SPR-1": sprint}}
	svc := service.NewBoardService(boards, nil, sprints)

	cols, err := svc.FilterBoard(ctx, "bob", "SPR-1", alice)

	require.NoError(t, err)
	require.Len(t, cols["Open"], 1)
	assert.Equal(t, "ISS-1", cols["Open"][0].ID)
	assert.Equal(t, []string{
		"BoardRepository.FindByUser(alice)",
		"SprintRepository.FindByID(SPR-1)",
		"BoardRepository.Save(alice)",
	}, log.entries)
}

func TestSortBoard_OrdersAndPlacesEmptyValuesLast(t *testing.T) {
	log := &callLog{}
	alice := &domain.User{ID: "alice"}
	open := &domain.Status{ID: "open", Name: "Open"}
	d1, d2 := day("2026-10-15"), day("2026-10-25")

	iNoDue := &domain.Issue{ID: "ISS-1", Status: open, Priority: &domain.Priority{Name: "High"}, DueDate: nil}
	iLate := &domain.Issue{ID: "ISS-2", Status: open, Priority: &domain.Priority{Name: "Low"}, DueDate: &d2}
	iEarly := &domain.Issue{ID: "ISS-3", Status: open, Priority: &domain.Priority{Name: "Medium"}, DueDate: &d1}

	board := domain.NewBoard("alice", []*domain.Issue{iNoDue, iLate, iEarly})
	boards := &woBoardRepo{log: log, boards: map[string]*domain.Board{"alice": board}}
	issues := &woIssueRepo{log: log}
	svc := service.NewBoardService(boards, issues, nil)

	// Sort by dueDate ascending: earliest first, nil dueDate at the end
	cols, err := svc.SortBoard(ctx, "dueDate", "asc", alice)
	require.NoError(t, err)
	require.Len(t, cols["Open"], 3)
	assert.Equal(t, []string{"ISS-3", "ISS-2", "ISS-1"}, []string{
		cols["Open"][0].ID, cols["Open"][1].ID, cols["Open"][2].ID,
	})
	assert.Equal(t, []string{
		"BoardRepository.FindByUser(alice)",
		"IssueRepository.FindAllSorted(dueDate, asc)",
		"BoardRepository.Save(alice)",
	}, log.entries)

	// Invalid attribute returns validation error
	_, err = svc.SortBoard(ctx, "unknownField", "asc", alice)
	assert.Contains(t, fieldsOf(t, err), "attribute")
}

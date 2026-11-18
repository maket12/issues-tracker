package repository

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

type SprintRepo struct {
	db *sqlx.DB
}

func NewSprintRepo(db *sqlx.DB) *SprintRepo {
	return &SprintRepo{db: db}
}

func (r *SprintRepo) FindByIssue(ctx context.Context, issue *domain.Issue) (*domain.Sprint, error) {
	if issue == nil || issue.ID == "" {
		return nil, domain.ErrNotFound
	}
	var sprintID string
	err := r.db.GetContext(ctx, &sprintID,
		"SELECT s.id FROM Sprints s "+
			"JOIN SprintAssignments sa ON sa.Sprintskey = s.`key` "+
			"JOIN Issue i ON i.`key` = sa.Issuekey "+
			"WHERE i.id = ? LIMIT 1", issue.ID)
	if err != nil {
		return nil, translate(err)
	}
	return findSprint(ctx, r.db, sprintID)
}

func (r *SprintRepo) Save(ctx context.Context, sprint *domain.Sprint) (*domain.Sprint, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	endDate := sprint.StartDate()
	if sprint.EndDate != nil {
		endDate = *sprint.EndDate
	}
	closedBit := 0
	if sprint.IsClosed() {
		closedBit = 1
	}

	id := sprint.ID
	var sprintKey int64
	if id == "" {
		tempID, err := randomHex(16)
		if err != nil {
			return nil, err
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO Sprints (id, `start`, `end`, closed) VALUES (?, ?, ?, ?)",
			tempID, sprint.StartDate(), endDate, closedBit)
		if err != nil {
			return nil, translate(err)
		}
		sprintKey, err = res.LastInsertId()
		if err != nil {
			return nil, err
		}
		id = fmt.Sprintf("SPR-%d", sprintKey)
		if _, err := tx.ExecContext(ctx, "UPDATE Sprints SET id = ? WHERE `key` = ?", id, sprintKey); err != nil {
			return nil, translate(err)
		}
	} else {
		if err := tx.GetContext(ctx, &sprintKey, "SELECT `key` FROM Sprints WHERE id = ?", id); err != nil {
			return nil, translate(err)
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE Sprints SET `start` = ?, `end` = ?, closed = ? WHERE `key` = ?",
			sprint.StartDate(), endDate, closedBit, sprintKey); err != nil {
			return nil, translate(err)
		}
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM SprintAssignments WHERE Sprintskey = ?", sprintKey); err != nil {
		return nil, translate(err)
	}
	for _, iss := range sprint.GetIssues() {
		if iss == nil || iss.ID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO SprintAssignments (Sprintskey, Issuekey) VALUES (?, (SELECT `key` FROM Issue WHERE id = ?))",
			sprintKey, iss.ID); err != nil {
			return nil, translate(err)
		}
	}

	saved, err := findSprint(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return saved, nil
}

func findSprint(ctx context.Context, q sqlx.QueryerContext, id string) (*domain.Sprint, error) {
	var row struct {
		Key    int64       `db:"key"`
		ID     string      `db:"id"`
		Start  domain.Date `db:"start"`
		End    domain.Date `db:"end"`
		Closed []byte      `db:"closed"`
	}
	if err := sqlx.GetContext(ctx, q, &row, "SELECT `key`, id, `start`, `end`, closed FROM Sprints WHERE id = ?", id); err != nil {
		return nil, translate(err)
	}

	var issueIDs []string
	if err := sqlx.SelectContext(ctx, q, &issueIDs,
		"SELECT i.id FROM Issue i JOIN SprintAssignments sa ON sa.Issuekey = i.`key` WHERE sa.Sprintskey = ? ORDER BY i.`key`",
		row.Key); err != nil {
		return nil, err
	}

	issues := make([]*domain.Issue, 0, len(issueIDs))
	for _, issID := range issueIDs {
		iss, err := findIssue(ctx, q, issID)
		if err != nil {
			return nil, err
		}
		issues = append(issues, iss)
	}

	closed := len(row.Closed) > 0 && row.Closed[0] != 0
	endCopy := row.End
	return domain.RestoreSprint(row.ID, row.ID, row.Start, &endCopy, closed, issues), nil
}

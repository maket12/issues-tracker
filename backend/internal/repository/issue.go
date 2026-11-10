package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

type IssueRepo struct{ 
	db *sqlx.DB 
}

func NewIssueRepo(db *sqlx.DB) *IssueRepo { 
	return &IssueRepo{db: db} 
}

func (r *IssueRepo) FindByID(ctx context.Context, id string) (*domain.Issue, error) {
	return findIssue(ctx, r.db, id)
}

func (r *IssueRepo) FindTypeByID(ctx context.Context, id string) (*domain.IssueType, error) {
	var row struct {
		ID          string         `db:"id"`
		Name        string         `db:"name"`
		Description sql.NullString `db:"description"`
	}
	if err := r.db.GetContext(ctx, &row, "SELECT id, name, description FROM IssueTypes WHERE id = ?", id); err != nil {
		return nil, translate(err)
	}
	return &domain.IssueType{ID: row.ID, Name: row.Name, Description: nullable(row.Description)}, nil
}

func (r *IssueRepo) FindPriorityByID(ctx context.Context, id string) (*domain.Priority, error) {
	var row struct {
		ID          string         `db:"id"`
		Name        string         `db:"name"`
		Description sql.NullString `db:"description"`
	}
	if err := r.db.GetContext(ctx, &row, "SELECT id, name, description FROM Priorities WHERE id = ?", id); err != nil {
		return nil, translate(err)
	}
	return &domain.Priority{ID: row.ID, Name: row.Name, Description: nullable(row.Description)}, nil
}

func (r *IssueRepo) FindUserByID(ctx context.Context, id string) (*domain.User, error) {
	return findUser(ctx, r.db, id)
}

func (r *IssueRepo) FindInitialStatus(ctx context.Context) (*domain.Status, error) {
	var s domain.Status
	var row struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}
	if err := r.db.GetContext(ctx, &row, "SELECT id, name FROM Statuses ORDER BY `key` LIMIT 1"); err != nil {
		return nil, translate(err)
	}
	s.ID, s.Name = row.ID, row.Name
	return &s, nil
}

func (r *IssueRepo) Save(ctx context.Context, issue *domain.Issue) (*domain.Issue, error) {
	tx, err := r.db.BeginTxx(ctx, nil)

	if err != nil {
		return nil, err
	}

	defer func() { 
		_ = tx.Rollback() 
	}()

	var id string
	if issue.ID == "" {
		id, err = insertIssue(ctx, tx, issue)
	} else {
		id = issue.ID
		err = updateIssue(ctx, tx, issue)
	}
	if err != nil {
		return nil, err
	}

	saved, err := findIssue(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return saved, nil
}

const insertIssueSQL = "INSERT INTO Issue " +
	"(id, type, name, description, author, assignee, status, priority, startDate, dueDate, version) VALUES (" +
	"?, " +
	"(SELECT `key` FROM IssueTypes WHERE id = ?), " +
	"?, ?, " +
	"(SELECT `key` FROM Users WHERE id = ?), " +
	"(SELECT `key` FROM Users WHERE id = ?), " +
	"(SELECT `key` FROM Statuses WHERE id = ?), " +
	"COALESCE((SELECT `key` FROM Priorities WHERE id = ?), (SELECT `key` FROM Priorities ORDER BY `key` LIMIT 1)), " +
	"?, ?, ?)"

func insertIssue(ctx context.Context, tx *sqlx.Tx, i *domain.Issue) (string, error) {
	tempID, err := randomHex(16)
	if err != nil {
		return "", err
	}
	version, err := randomHex(8)
	if err != nil {
		return "", err
	}
	res, err := tx.ExecContext(ctx, insertIssueSQL,
		tempID, i.Type.ID, i.Name, i.Description, i.Author.ID, userID(i.Assignee), i.Status.ID, priorityID(i.Priority),
		i.StartDate, i.DueDate, version)
	if err != nil {
		return "", translate(err)
	}
	key, err := res.LastInsertId()
	if err != nil {
		return "", err
	}
	id := fmt.Sprintf("ISS-%d", key)
	if _, err := tx.ExecContext(ctx, "UPDATE Issue SET id = ? WHERE `key` = ?", id, key); err != nil {
		return "", translate(err)
	}
	return id, nil
}

const updateIssueSQL = "UPDATE Issue SET " +
	"name = ?, description = ?, " +
	"type = (SELECT `key` FROM IssueTypes WHERE id = ?), " +
	"assignee = (SELECT `key` FROM Users WHERE id = ?), " +
	"priority = (SELECT `key` FROM Priorities WHERE id = ?), " +
	"startDate = ?, dueDate = ?, version = ? " +
	"WHERE id = ? AND version = ?"

func updateIssue(ctx context.Context, tx *sqlx.Tx, i *domain.Issue) error {
	newVersion, err := randomHex(8)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, updateIssueSQL,
		i.Name, i.Description, i.Type.ID, userID(i.Assignee), priorityID(i.Priority),
		i.StartDate, i.DueDate, newVersion, i.ID, i.Version)
	if err != nil {
		return translate(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrConflict
	}
	return nil
}

func userID(u *domain.User) any {
	if u == nil {
		return nil
	}
	return u.ID
}

func priorityID(p *domain.Priority) any {
	if p == nil {
		return nil
	}
	return p.ID
}

const issueSelect = "SELECT i.id, i.name, i.description, i.startDate, i.dueDate, i.version, " +
	"t.id AS typeId, t.name AS typeName, t.description AS typeDescription, " +
	"p.id AS priorityId, p.name AS priorityName, p.description AS priorityDescription, " +
	"s.id AS statusId, s.name AS statusName, " +
	"au.id AS authorId, au.name AS authorName, " +
	"asg.id AS assigneeId, asg.name AS assigneeName " +
	"FROM Issue i " +
	"JOIN IssueTypes t ON t.`key` = i.type " +
	"JOIN Priorities p ON p.`key` = i.priority " +
	"JOIN Statuses s ON s.`key` = i.status " +
	"JOIN Users au ON au.`key` = i.author " +
	"LEFT JOIN Users asg ON asg.`key` = i.assignee "

type issueRow struct {
	ID                  string         `db:"id"`
	Name                string         `db:"name"`
	Description         sql.NullString `db:"description"`
	StartDate           domain.Date    `db:"startDate"`
	DueDate             *domain.Date   `db:"dueDate"`
	Version             string         `db:"version"`
	TypeID              string         `db:"typeId"`
	TypeName            string         `db:"typeName"`
	TypeDescription     sql.NullString `db:"typeDescription"`
	PriorityID          string         `db:"priorityId"`
	PriorityName        string         `db:"priorityName"`
	PriorityDescription sql.NullString `db:"priorityDescription"`
	StatusID            string         `db:"statusId"`
	StatusName          string         `db:"statusName"`
	AuthorID            string         `db:"authorId"`
	AuthorName          string         `db:"authorName"`
	AssigneeID          sql.NullString `db:"assigneeId"`
	AssigneeName        sql.NullString `db:"assigneeName"`
}

func findIssue(ctx context.Context, q sqlx.QueryerContext, id string) (*domain.Issue, error) {
	var row issueRow
	if err := sqlx.GetContext(ctx, q, &row, issueSelect+"WHERE i.id = ?", id); err != nil {
		return nil, translate(err)
	}
	issue := &domain.Issue{
		ID:          row.ID,
		Name:        row.Name,
		Description: nullable(row.Description),
		Type:        &domain.IssueType{ID: row.TypeID, Name: row.TypeName, Description: nullable(row.TypeDescription)},
		Priority:    &domain.Priority{ID: row.PriorityID, Name: row.PriorityName, Description: nullable(row.PriorityDescription)},
		Status:      &domain.Status{ID: row.StatusID, Name: row.StatusName},
		Author:      &domain.User{ID: row.AuthorID, Name: row.AuthorName},
		StartDate:   row.StartDate,
		DueDate:     row.DueDate,
		Version:     row.Version,
	}
	if row.AssigneeID.Valid {
		issue.Assignee = &domain.User{ID: row.AssigneeID.String, Name: row.AssigneeName.String}
	}
	return issue, nil
}

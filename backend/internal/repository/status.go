package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// StatusRepo implements service.StatusRepository.
type StatusRepo struct{ db *sqlx.DB }

func NewStatusRepo(db *sqlx.DB) *StatusRepo { return &StatusRepo{db: db} }

func (r *StatusRepo) FindByID(ctx context.Context, id string) (*domain.Status, error) {
	return findStatus(ctx, r.db, id)
}

// Save inserts a status that has no id yet (its id becomes STS-<n> and is also set on the given object)
// and renames one that has. A name that is already taken gives domain.ErrStatusExists; the database
// compares names case- and accent-insensitively.
func (r *StatusRepo) Save(ctx context.Context, status *domain.Status) (*domain.Status, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	id := status.ID
	if id == "" {
		// the readable id is derived from the generated key, so the row first gets a temporary id
		tempID, err := randomHex(16)
		if err != nil {
			return nil, err
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO Statuses (id, name) VALUES (?, ?)", tempID, status.Name)
		if err != nil {
			return nil, translateStatus(err)
		}
		key, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		id = fmt.Sprintf("STS-%d", key)
		if _, err := tx.ExecContext(ctx, "UPDATE Statuses SET id = ? WHERE `key` = ?", id, key); err != nil {
			return nil, translateStatus(err)
		}
	} else if _, err := tx.ExecContext(ctx, "UPDATE Statuses SET name = ? WHERE id = ?", status.Name, id); err != nil {
		return nil, translateStatus(err)
	}

	saved, err := findStatus(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	status.ID = saved.ID
	return saved, nil
}

func findStatus(ctx context.Context, q sqlx.QueryerContext, id string) (*domain.Status, error) {
	var row struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}
	if err := sqlx.GetContext(ctx, q, &row, "SELECT id, name FROM Statuses WHERE id = ?", id); err != nil {
		return nil, translate(err)
	}
	return &domain.Status{ID: row.ID, Name: row.Name}, nil
}

func translateStatus(err error) error {
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 1062 && strings.Contains(me.Message, "uq_statuses_name") {
		return domain.ErrStatusExists
	}
	return translate(err)
}

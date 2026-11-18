package repository

import (
	"context"

	"github.com/jmoiron/sqlx"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

type RoleRepo struct {
	db *sqlx.DB
}

func NewRoleRepo(db *sqlx.DB) *RoleRepo {
	return &RoleRepo{db: db}
}

func (r *RoleRepo) FindDefaultRole(ctx context.Context) (*domain.Role, error) {
	return findRole(ctx, r.db, "1=1 ORDER BY `key` LIMIT 1")
}

func (r *RoleRepo) FindByID(ctx context.Context, id string) (*domain.Role, error) {
	return findRole(ctx, r.db, "id = ?", id)
}

func findRole(ctx context.Context, q sqlx.QueryerContext, clause string, args ...any) (*domain.Role, error) {
	var row struct {
		Key  int64  `db:"key"`
		ID   string `db:"id"`
		Name string `db:"name"`
	}
	if err := sqlx.GetContext(ctx, q, &row, "SELECT `key`, id, name FROM Roles WHERE "+clause, args...); err != nil {
		return nil, translate(err)
	}

	var perms []string
	if err := sqlx.SelectContext(ctx, q, &perms, "SELECT permission FROM RolesPermissions WHERE role = ?", row.Key); err != nil {
		return nil, err
	}

	role := &domain.Role{ID: row.ID, Name: row.Name}
	for _, p := range perms {
		role.Permissions = append(role.Permissions, domain.Permission(p))
	}
	return role, nil
}

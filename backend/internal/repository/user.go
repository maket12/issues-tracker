package repository

import (
	"context"

	"github.com/jmoiron/sqlx"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

type UserRepo struct { 
	db *sqlx.DB 
}

func NewUserRepo(db *sqlx.DB) *UserRepo { 
	return &UserRepo{db: db} 
}

func (r *UserRepo) FindByID(ctx context.Context, id string) (*domain.User, error) {
	return findUser(ctx, r.db, id)
}

func findUser(ctx context.Context, q sqlx.QueryerContext, id string) (*domain.User, error) {
	var row struct {
		ID       string `db:"id"`
		Name     string `db:"name"`
		Email    string `db:"email"`
		RoleKey  int64  `db:"roleKey"`
		RoleID   string `db:"roleId"`
		RoleName string `db:"roleName"`
	}
	
	err := sqlx.GetContext(ctx, q, &row,
		"SELECT u.id, u.name, u.email, r.`key` AS roleKey, r.id AS roleId, r.name AS roleName "+
			"FROM Users u JOIN Roles r ON r.`key` = u.role WHERE u.id = ?", id)

	if err != nil {
		return nil, translate(err)
	}

	var perms []string
	if err := sqlx.SelectContext(ctx, q, &perms,
		"SELECT permission FROM RolesPermissions WHERE role = ?", row.RoleKey); err != nil {
		return nil, err
	}

	role := &domain.Role{
		ID: row.RoleID, 
		Name: row.RoleName,
	}

	for _, p := range perms {
		role.Permissions = append(role.Permissions, domain.Permission(p))
	}

	return &domain.User{
		ID: row.ID, 
		Name: row.Name, 
		Email: row.Email, 
		Role: role,
	}, nil
}

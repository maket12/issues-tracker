package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
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
	return findUser(ctx, r.db, "u.id = ?", id)
}

func (r *UserRepo) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	return findUser(ctx, r.db, "u.email = ?", email)
}

func (r *UserRepo) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	var count int
	if err := r.db.GetContext(ctx, &count, "SELECT COUNT(*) FROM Users WHERE email = ?", email); err != nil {
		return false, translate(err)
	}
	return count > 0, nil
}

func (r *UserRepo) Save(ctx context.Context, user *domain.User) (*domain.User, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	id := user.ID
	if id == "" {
		tempID, err := randomHex(16)
		if err != nil {
			return nil, err
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO Users (id, name, email, passwdHash, role) VALUES (?, ?, ?, ?, (SELECT `key` FROM Roles WHERE id = ?))",
			tempID, user.Name, user.Email, user.PasswdHash, user.Role.ID)
		if err != nil {
			var me *mysql.MySQLError
			if errors.As(err, &me) && me.Number == 1062 {
				return nil, domain.ErrEmailTaken
			}
			return nil, translate(err)
		}
		key, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		id = fmt.Sprintf("USR-%d", key)
		if _, err := tx.ExecContext(ctx, "UPDATE Users SET id = ? WHERE `key` = ?", id, key); err != nil {
			return nil, translate(err)
		}
	} else {
		res, err := tx.ExecContext(ctx,
			"UPDATE Users SET name = ?, email = ?, role = (SELECT `key` FROM Roles WHERE id = ?) WHERE id = ?",
			user.Name, user.Email, user.Role.ID, user.ID)
		if err != nil {
			var me *mysql.MySQLError
			if errors.As(err, &me) && me.Number == 1062 {
				return nil, domain.ErrEmailTaken
			}
			return nil, translate(err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n == 0 {
			var exists int
			if err := tx.GetContext(ctx, &exists, "SELECT COUNT(*) FROM Users WHERE id = ?", user.ID); err != nil {
				return nil, translate(err)
			}
			if exists == 0 {
				return nil, domain.ErrNotFound
			}
		}
	}

	saved, err := findUser(ctx, tx, "u.id = ?", id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return saved, nil
}

func findUser(ctx context.Context, q sqlx.QueryerContext, whereClause string, arg any) (*domain.User, error) {
	var row struct {
		ID         string `db:"id"`
		Name       string `db:"name"`
		Email      string `db:"email"`
		PasswdHash string `db:"passwdHash"`
		RoleKey    int64  `db:"roleKey"`
		RoleID     string `db:"roleId"`
		RoleName   string `db:"roleName"`
	}

	err := sqlx.GetContext(ctx, q, &row,
		"SELECT u.id, u.name, u.email, u.passwdHash, r.`key` AS roleKey, r.id AS roleId, r.name AS roleName "+
			"FROM Users u JOIN Roles r ON r.`key` = u.role WHERE "+whereClause, arg)

	if err != nil {
		return nil, translate(err)
	}

	var perms []string
	if err := sqlx.SelectContext(ctx, q, &perms,
		"SELECT permission FROM RolesPermissions WHERE role = ?", row.RoleKey); err != nil {
		return nil, err
	}

	role := &domain.Role{
		ID:   row.RoleID,
		Name: row.RoleName,
	}

	for _, p := range perms {
		role.Permissions = append(role.Permissions, domain.Permission(p))
	}

	return &domain.User{
		ID:         row.ID,
		Name:       row.Name,
		Email:      row.Email,
		PasswdHash: row.PasswdHash,
		Role:       role,
	}, nil
}

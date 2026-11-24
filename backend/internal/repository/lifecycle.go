package repository

import (
	"context"

	"github.com/jmoiron/sqlx"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

// LifecycleRepo implements service.LifecycleRepository on Statuses, TransitionRules and
// TransitionRules_Roles.
type LifecycleRepo struct{ db *sqlx.DB }

func NewLifecycleRepo(db *sqlx.DB) *LifecycleRepo { return &LifecycleRepo{db: db} }

func (r *LifecycleRepo) GetLifecycle(ctx context.Context) (*domain.Lifecycle, error) {
	return loadLifecycle(ctx, r.db)
}

const upsertRuleSQL = "INSERT INTO TransitionRules (`from`, `to`, allowAuthor) VALUES (" +
	"(SELECT `key` FROM Statuses WHERE id = ?), (SELECT `key` FROM Statuses WHERE id = ?), ?) AS new " +
	"ON DUPLICATE KEY UPDATE allowAuthor = new.allowAuthor"

const ruleKeySQL = "SELECT `key` FROM TransitionRules " +
	"WHERE `from` = (SELECT `key` FROM Statuses WHERE id = ?) AND `to` = (SELECT `key` FROM Statuses WHERE id = ?)"

// A plain INSERT IGNORE would silently skip an unknown role; this no-op update keeps real errors visible.
const linkRoleSQL = "INSERT INTO TransitionRules_Roles (TransitionRulekey, Rolekey) VALUES (" +
	"?, (SELECT `key` FROM Roles WHERE id = ?)) AS new ON DUPLICATE KEY UPDATE Rolekey = new.Rolekey"

// Save writes every transition of the lifecycle (creating it or updating its allowAuthor flag) and links
// its roles, all in one transaction, and returns the lifecycle as stored. Statuses must already exist.
// It never deletes: see service.LifecycleRepository.
func (r *LifecycleRepo) Save(ctx context.Context, lifecycle *domain.Lifecycle) (*domain.Lifecycle, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	for _, rule := range lifecycle.GetRules() {
		if _, err := tx.ExecContext(ctx, upsertRuleSQL, rule.From.ID, rule.To.ID, rule.AllowsAuthor()); err != nil {
			return nil, translate(err)
		}
		var ruleKey int64
		if err := tx.GetContext(ctx, &ruleKey, ruleKeySQL, rule.From.ID, rule.To.ID); err != nil {
			return nil, translate(err)
		}
		for _, role := range rule.GetAllowedRoles() {
			if _, err := tx.ExecContext(ctx, linkRoleSQL, ruleKey, role.ID); err != nil {
				return nil, translate(err)
			}
		}
	}

	saved, err := loadLifecycle(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return saved, nil
}

// loadLifecycle reads the whole graph with three queries: statuses, transitions, and their roles.
// Roles are loaded with id and name only; the permissions of a role are not needed to check a transition.
func loadLifecycle(ctx context.Context, q sqlx.QueryerContext) (*domain.Lifecycle, error) {
	var statusRows []struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}
	if err := sqlx.SelectContext(ctx, q, &statusRows, "SELECT id, name FROM Statuses ORDER BY `key`"); err != nil {
		return nil, err
	}
	statuses := make([]*domain.Status, 0, len(statusRows))
	byID := make(map[string]*domain.Status, len(statusRows))
	for _, row := range statusRows {
		s := &domain.Status{ID: row.ID, Name: row.Name}
		statuses = append(statuses, s)
		byID[s.ID] = s
	}

	var ruleRows []struct {
		Key         int64  `db:"key"`
		FromID      string `db:"fromId"`
		ToID        string `db:"toId"`
		AllowAuthor bool   `db:"allowAuthor"`
	}
	if err := sqlx.SelectContext(ctx, q, &ruleRows,
		"SELECT tr.`key`, sf.id AS fromId, st.id AS toId, tr.allowAuthor "+
			"FROM TransitionRules tr "+
			"JOIN Statuses sf ON sf.`key` = tr.`from` "+
			"JOIN Statuses st ON st.`key` = tr.`to` "+
			"ORDER BY tr.`key`"); err != nil {
		return nil, err
	}

	var roleRows []struct {
		RuleKey int64  `db:"ruleKey"`
		ID      string `db:"id"`
		Name    string `db:"name"`
	}
	if err := sqlx.SelectContext(ctx, q, &roleRows,
		"SELECT trr.TransitionRulekey AS ruleKey, r.id, r.name "+
			"FROM TransitionRules_Roles trr JOIN Roles r ON r.`key` = trr.Rolekey "+
			"ORDER BY trr.TransitionRulekey, r.`key`"); err != nil {
		return nil, err
	}
	rolesByRule := make(map[int64][]*domain.Role)
	for _, row := range roleRows {
		rolesByRule[row.RuleKey] = append(rolesByRule[row.RuleKey], &domain.Role{ID: row.ID, Name: row.Name})
	}

	rules := make([]*domain.TransitionRule, 0, len(ruleRows))
	for _, row := range ruleRows {
		rules = append(rules, domain.RestoreTransitionRule(byID[row.FromID], byID[row.ToID], row.AllowAuthor, rolesByRule[row.Key]))
	}
	return domain.RestoreLifecycle(statuses, rules), nil
}

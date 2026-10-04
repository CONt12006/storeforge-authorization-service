package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/storeforge/authorization-service/internal/domain"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, databaseURL string) (*Repository, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	repository := &Repository{pool: pool}
	if err := repository.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return repository, nil
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *Repository) Close() {
	r.pool.Close()
}

func (r *Repository) GetEffectiveAccess(ctx context.Context, userID int64) (domain.EffectiveAccess, error) {
	access := domain.EffectiveAccess{}
	permissions, err := r.pool.Query(ctx, `
		SELECT DISTINCT p.id, p.resource, p.action
		FROM permissions p
		JOIN role_permissions rp ON rp.permission_id = p.id
		JOIN user_roles ur ON ur.role_id = rp.role_id
		WHERE ur.user_id = $1
		ORDER BY p.resource, p.action
	`, userID)
	if err != nil {
		return access, err
	}
	for permissions.Next() {
		var permission domain.Permission
		if err := permissions.Scan(&permission.ID, &permission.Resource, &permission.Action); err != nil {
			permissions.Close()
			return access, err
		}
		access.Permissions = append(access.Permissions, permission)
	}
	if err := permissions.Err(); err != nil {
		permissions.Close()
		return access, err
	}
	permissions.Close()
	policies, err := r.pool.Query(ctx, `
		SELECT DISTINCT p.id, r.name, p.resource, p.action, p.effect, p.condition_type
		FROM policies p
		JOIN roles r ON r.id = p.role_id
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = $1
		ORDER BY p.id
	`, userID)
	if err != nil {
		return access, err
	}
	defer policies.Close()
	for policies.Next() {
		var policy domain.Policy
		if err := policies.Scan(&policy.ID, &policy.RoleName, &policy.Resource, &policy.Action, &policy.Effect, &policy.ConditionType); err != nil {
			return access, err
		}
		access.Policies = append(access.Policies, policy)
	}
	return access, policies.Err()
}

func (r *Repository) GetUserRoles(ctx context.Context, userID int64) ([]domain.Role, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT r.id, r.name, r.created_at
		FROM roles r
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = $1
		ORDER BY r.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := make([]domain.Role, 0)
	for rows.Next() {
		var role domain.Role
		if err := rows.Scan(&role.ID, &role.Name, &role.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (r *Repository) ReplaceUserRoles(ctx context.Context, userID int64, names []string) error {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(ctx)
	if _, err := transaction.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, name := range names {
		result, err := transaction.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id)
			SELECT $1, id FROM roles WHERE name = $2
			ON CONFLICT DO NOTHING
		`, userID, name)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			var exists bool
			if err := transaction.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE name = $1)`, name).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("role %s not found", name)
			}
		}
	}
	return transaction.Commit(ctx)
}

func (r *Repository) ListRoles(ctx context.Context) ([]domain.Role, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, created_at FROM roles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := make([]domain.Role, 0)
	for rows.Next() {
		var role domain.Role
		if err := rows.Scan(&role.ID, &role.Name, &role.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (r *Repository) CreateRole(ctx context.Context, name string) (domain.Role, error) {
	var role domain.Role
	err := r.pool.QueryRow(ctx, `
		INSERT INTO roles (name) VALUES ($1)
		RETURNING id, name, created_at
	`, name).Scan(&role.ID, &role.Name, &role.CreatedAt)
	return role, err
}

func (r *Repository) AddPermissionToRole(ctx context.Context, roleName string, request domain.AddPermissionRequest) error {
	result, err := r.pool.Exec(ctx, `
		WITH role_row AS (
			SELECT id FROM roles WHERE name = $1
		), permission_row AS (
			INSERT INTO permissions (resource, action)
			VALUES ($2, $3)
			ON CONFLICT (resource, action) DO UPDATE SET resource = EXCLUDED.resource
			RETURNING id
		)
		INSERT INTO role_permissions (role_id, permission_id)
		SELECT role_row.id, permission_row.id FROM role_row, permission_row
		ON CONFLICT DO NOTHING
	`, roleName, request.Resource, request.Action)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		var exists bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE name = $1)`, roleName).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("role %s not found", roleName)
		}
	}
	return nil
}

func (r *Repository) AddPolicyToRole(ctx context.Context, roleName string, request domain.AddPolicyRequest) error {
	result, err := r.pool.Exec(ctx, `
		INSERT INTO policies (role_id, resource, action, effect, condition_type)
		SELECT id, $2, $3, $4, $5 FROM roles WHERE name = $1
		ON CONFLICT (role_id, resource, action, effect, condition_type) DO NOTHING
	`, roleName, request.Resource, request.Action, request.Effect, request.ConditionType)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		var exists bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE name = $1)`, roleName).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("role %s not found", roleName)
		}
	}
	return nil
}

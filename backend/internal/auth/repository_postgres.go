package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SV1Stail/online_market/internal/apperror"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) GetByLogin(ctx context.Context, login string) (*User, error) {
	return r.getUser(ctx, `WHERE lower(u.login) = lower($1)`, login)
}

func (r *PostgresRepository) CreateSession(ctx context.Context, session Session) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO admin_sessions (
			id, admin_user_id, refresh_token_hash, expires_at, created_at
		) VALUES ($1::uuid, $2::uuid, $3, $4, $5)
	`, session.ID, session.UserID, session.RefreshTokenHash, session.ExpiresAt, session.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert admin session: %w", err)
	}
	return nil
}

func (r *PostgresRepository) RotateSession(
	ctx context.Context,
	oldHash []byte,
	newHash []byte,
	expiresAt time.Time,
	now time.Time,
) (*User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin session rotation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID string
	err = tx.QueryRow(ctx, `
		UPDATE admin_sessions
		SET refresh_token_hash = $1, expires_at = $2
		WHERE refresh_token_hash = $3
			AND revoked_at IS NULL
			AND expires_at > $4
		RETURNING admin_user_id::text
	`, newHash, expiresAt, oldHash, now).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.ErrUnauthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("rotate admin session: %w", err)
	}
	user, err := getUserWithQuerier(ctx, tx, `WHERE u.id = $1::uuid`, userID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit session rotation: %w", err)
	}
	return user, nil
}

func (r *PostgresRepository) RevokeSession(ctx context.Context, tokenHash []byte, now time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE admin_sessions
		SET revoked_at = $2
		WHERE refresh_token_hash = $1 AND revoked_at IS NULL
	`, tokenHash, now)
	if err != nil {
		return fmt.Errorf("update admin session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.ErrUnauthenticated
	}
	return nil
}

func (r *PostgresRepository) CreateUser(ctx context.Context, user User) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create admin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		INSERT INTO admin_users (id, login, password_hash, display_name, is_active)
		VALUES ($1::uuid, $2, $3, $4, $5)
	`, user.ID, user.Login, user.PasswordHash, user.DisplayName, user.Active)
	if err != nil {
		return fmt.Errorf("insert admin user: %w", err)
	}
	for _, role := range user.Roles {
		if _, err := tx.Exec(ctx, `
			INSERT INTO admin_user_roles (admin_user_id, role)
			VALUES ($1::uuid, $2)
		`, user.ID, role); err != nil {
			return fmt.Errorf("insert admin role: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create admin: %w", err)
	}
	return nil
}

func (r *PostgresRepository) getUser(ctx context.Context, condition string, argument any) (*User, error) {
	return getUserWithQuerier(ctx, r.pool, condition, argument)
}

type userQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getUserWithQuerier(ctx context.Context, query userQuerier, condition string, argument any) (*User, error) {
	var user User
	var roles []string
	statement := `
		SELECT
			u.id::text,
			u.login,
			u.password_hash,
			u.display_name,
			u.is_active,
			COALESCE(array_agg(r.role ORDER BY r.role) FILTER (WHERE r.role IS NOT NULL), ARRAY[]::text[])
		FROM admin_users u
		LEFT JOIN admin_user_roles r ON r.admin_user_id = u.id
		` + condition + `
		GROUP BY u.id
	`
	err := query.QueryRow(ctx, statement, argument).Scan(
		&user.ID,
		&user.Login,
		&user.PasswordHash,
		&user.DisplayName,
		&user.Active,
		&roles,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query admin user: %w", err)
	}
	user.Roles = make([]Role, len(roles))
	for index, role := range roles {
		user.Roles[index] = Role(role)
	}
	return &user, nil
}

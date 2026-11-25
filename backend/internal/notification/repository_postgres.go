package notification

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Claim(ctx context.Context, now time.Time, lockTimeout time.Duration) (*Message, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin notification claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE notification_outbox
		SET status = 'pending', locked_at = NULL, available_at = $1, updated_at = $1
		WHERE status = 'processing' AND locked_at < $2
	`, now, now.Add(-lockTimeout)); err != nil {
		return nil, fmt.Errorf("recover stale notifications: %w", err)
	}
	var message Message
	err = tx.QueryRow(ctx, `
		WITH selected AS (
			SELECT id
			FROM notification_outbox
			WHERE status = 'pending' AND available_at <= $1
			ORDER BY available_at, created_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE notification_outbox n
		SET status = 'processing',
			locked_at = $1,
			attempts = attempts + 1,
			updated_at = $1
		FROM selected
		WHERE n.id = selected.id
		RETURNING n.id::text, n.order_id::text, n.notification_type,
			n.channel, n.payload, n.attempts
	`, now).Scan(
		&message.ID,
		&message.OrderID,
		&message.Type,
		&message.Channel,
		&message.Payload,
		&message.Attempts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty notification claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim outbox row: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit notification claim: %w", err)
	}
	return &message, nil
}

func (r *PostgresRepository) MarkSent(ctx context.Context, id string, now time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE notification_outbox
		SET status = 'sent', sent_at = $2, locked_at = NULL,
			last_error = '', updated_at = $2
		WHERE id = $1::uuid AND status = 'processing'
	`, id, now)
	if err != nil {
		return fmt.Errorf("update sent outbox row: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("notification is no longer processing")
	}
	return nil
}

func (r *PostgresRepository) MarkFailed(
	ctx context.Context,
	id string,
	attempts int,
	maxAttempts int,
	nextAttempt time.Time,
	lastError string,
) error {
	status := "pending"
	if attempts >= maxAttempts {
		status = "failed"
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE notification_outbox
		SET status = $2, available_at = $3, locked_at = NULL,
			last_error = $4, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1::uuid AND status = 'processing'
	`, id, status, nextAttempt, lastError)
	if err != nil {
		return fmt.Errorf("update failed outbox row: %w", err)
	}
	return nil
}

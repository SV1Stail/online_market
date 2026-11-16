package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SV1Stail/online_market/internal/observability"
)

func Open(ctx context.Context, databaseURL string, logger *slog.Logger) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		observability.LogError(ctx, logger, "postgres.parse_config", err)
		return nil, fmt.Errorf("parse database config: %w", err)
	}
	config.ConnConfig.Tracer = &queryTracer{logger: logger}
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	config.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		observability.LogError(ctx, logger, "postgres.create_pool", err)
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		observability.LogError(ctx, logger, "postgres.ping", err)
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

type queryTracer struct {
	logger *slog.Logger
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err == nil || errors.Is(data.Err, pgx.ErrNoRows) {
		return
	}
	observability.LogError(
		ctx,
		t.logger,
		"postgres.query",
		data.Err,
		"command_tag", data.CommandTag.String(),
	)
}

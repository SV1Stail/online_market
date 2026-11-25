package cart

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/catalog"
	"github.com/SV1Stail/online_market/internal/money"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Create(ctx context.Context, cart Cart, tokenHash []byte) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO carts (
			id, token_hash, status, currency_code, version,
			created_at, updated_at, expires_at
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)
	`, cart.ID, tokenHash, cart.Status, cart.Total.Currency, cart.Version, cart.CreatedAt, cart.UpdatedAt, cart.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert cart: %w", err)
	}
	return nil
}

func (r *PostgresRepository) Get(ctx context.Context, cartID string, tokenHash []byte) (*Cart, error) {
	return getCart(ctx, r.pool, cartID, tokenHash)
}

func (r *PostgresRepository) AddItem(
	ctx context.Context,
	cartID string,
	tokenHash []byte,
	itemID string,
	productID string,
	quantity uint32,
	expectedVersion uint64,
) (*Cart, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin add item: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockMutableCart(ctx, tx, cartID, tokenHash, expectedVersion); err != nil {
		return nil, err
	}
	var productCurrency string
	var availability catalog.Availability
	if err := tx.QueryRow(ctx, `
		SELECT currency_code, availability
		FROM products
		WHERE id = $1::uuid
	`, productID).Scan(&productCurrency, &availability); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.ErrNotFound
		}
		return nil, fmt.Errorf("read product: %w", err)
	}
	if availability != catalog.AvailabilityAvailable {
		return nil, fmt.Errorf("%w: product is not available", apperror.ErrPrecondition)
	}
	var cartCurrency string
	if err := tx.QueryRow(ctx, `SELECT currency_code FROM carts WHERE id = $1::uuid`, cartID).Scan(&cartCurrency); err != nil {
		return nil, fmt.Errorf("read cart currency: %w", err)
	}
	if productCurrency != cartCurrency {
		return nil, fmt.Errorf("%w: product currency differs from cart currency", apperror.ErrPrecondition)
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO cart_items (id, cart_id, product_id, quantity)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4)
		ON CONFLICT (cart_id, product_id) DO UPDATE
		SET quantity = cart_items.quantity + EXCLUDED.quantity,
			updated_at = CURRENT_TIMESTAMP
		WHERE cart_items.quantity + EXCLUDED.quantity <= 10000
	`, itemID, cartID, productID, quantity)
	if err != nil {
		return nil, fmt.Errorf("upsert cart item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("%w: resulting quantity exceeds 10000", apperror.ErrPrecondition)
	}
	if err := bumpCart(ctx, tx, cartID); err != nil {
		return nil, err
	}
	result, err := getCart(ctx, tx, cartID, tokenHash)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit add item: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) UpdateItem(ctx context.Context, cartID string, tokenHash []byte, itemID string, quantity uint32, expectedVersion uint64) (*Cart, error) {
	return r.mutate(ctx, cartID, tokenHash, expectedVersion, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE cart_items
			SET quantity = $1, updated_at = CURRENT_TIMESTAMP
			WHERE id = $2::uuid AND cart_id = $3::uuid
		`, quantity, itemID, cartID)
		if err != nil {
			return fmt.Errorf("update cart item: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return apperror.ErrNotFound
		}
		return nil
	})
}

func (r *PostgresRepository) RemoveItem(ctx context.Context, cartID string, tokenHash []byte, itemID string, expectedVersion uint64) (*Cart, error) {
	return r.mutate(ctx, cartID, tokenHash, expectedVersion, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM cart_items WHERE id = $1::uuid AND cart_id = $2::uuid`, itemID, cartID)
		if err != nil {
			return fmt.Errorf("delete cart item: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return apperror.ErrNotFound
		}
		return nil
	})
}

func (r *PostgresRepository) Clear(ctx context.Context, cartID string, tokenHash []byte, expectedVersion uint64) (*Cart, error) {
	return r.mutate(ctx, cartID, tokenHash, expectedVersion, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM cart_items WHERE cart_id = $1::uuid`, cartID); err != nil {
			return fmt.Errorf("clear cart items: %w", err)
		}
		return nil
	})
}

func (r *PostgresRepository) mutate(
	ctx context.Context,
	cartID string,
	tokenHash []byte,
	expectedVersion uint64,
	mutation func(pgx.Tx) error,
) (*Cart, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cart mutation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockMutableCart(ctx, tx, cartID, tokenHash, expectedVersion); err != nil {
		return nil, err
	}
	if err := mutation(tx); err != nil {
		return nil, err
	}
	if err := bumpCart(ctx, tx, cartID); err != nil {
		return nil, err
	}
	result, err := getCart(ctx, tx, cartID, tokenHash)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cart mutation: %w", err)
	}
	return result, nil
}

func lockMutableCart(ctx context.Context, tx pgx.Tx, cartID string, tokenHash []byte, expectedVersion uint64) error {
	var status Status
	var version uint64
	var expiresAt time.Time
	err := tx.QueryRow(ctx, `
		SELECT status, version, expires_at
		FROM carts
		WHERE id = $1::uuid AND token_hash = $2
		FOR UPDATE
	`, cartID, tokenHash).Scan(&status, &version, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.ErrUnauthenticated
	}
	if err != nil {
		return fmt.Errorf("lock cart: %w", err)
	}
	if status == StatusOpen && !time.Now().UTC().Before(expiresAt) {
		return fmt.Errorf("%w: cart is expired", apperror.ErrPrecondition)
	}
	if status != StatusOpen {
		return fmt.Errorf("%w: cart is not open", apperror.ErrPrecondition)
	}
	if expectedVersion != 0 && expectedVersion != version {
		return apperror.ErrConflict
	}
	return nil
}

func bumpCart(ctx context.Context, tx pgx.Tx, cartID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE carts
		SET version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1::uuid
	`, cartID); err != nil {
		return fmt.Errorf("bump cart version: %w", err)
	}
	return nil
}

type cartQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func getCart(ctx context.Context, query cartQuerier, cartID string, tokenHash []byte) (*Cart, error) {
	var result Cart
	var currency string
	err := query.QueryRow(ctx, `
		SELECT id::text, status, currency_code, version, created_at, updated_at, expires_at
		FROM carts
		WHERE id = $1::uuid AND token_hash = $2
	`, cartID, tokenHash).Scan(
		&result.ID,
		&result.Status,
		&currency,
		&result.Version,
		&result.CreatedAt,
		&result.UpdatedAt,
		&result.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.ErrUnauthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("query cart: %w", err)
	}
	if result.Status == StatusOpen && !time.Now().UTC().Before(result.ExpiresAt) {
		result.Status = StatusExpired
	}
	result.Total, err = money.New(0, currency)
	if err != nil {
		return nil, fmt.Errorf("invalid stored cart currency: %w", err)
	}

	rows, err := query.Query(ctx, `
		SELECT
			ci.id::text,
			p.id::text,
			p.name,
			p.price_minor_units,
			p.currency_code,
			ci.quantity,
			p.availability
		FROM cart_items ci
		JOIN products p ON p.id = ci.product_id
		WHERE ci.cart_id = $1::uuid
		ORDER BY ci.created_at, ci.id
	`, cartID)
	if err != nil {
		return nil, fmt.Errorf("query cart items: %w", err)
	}
	defer rows.Close()

	result.Items = make([]Item, 0)
	for rows.Next() {
		var item Item
		var minorUnits int64
		var itemCurrency string
		if err := rows.Scan(
			&item.ID,
			&item.ProductID,
			&item.ProductName,
			&minorUnits,
			&itemCurrency,
			&item.Quantity,
			&item.Availability,
		); err != nil {
			return nil, fmt.Errorf("scan cart item: %w", err)
		}
		unitPrice, err := money.New(minorUnits, itemCurrency)
		if err != nil {
			return nil, fmt.Errorf("invalid cart item price: %w", err)
		}
		item.UnitPrice = unitPrice
		item.LineTotal, err = unitPrice.Multiply(item.Quantity)
		if err != nil {
			return nil, fmt.Errorf("calculate cart item total: %w", err)
		}
		result.Total, err = result.Total.Add(item.LineTotal)
		if err != nil {
			return nil, fmt.Errorf("calculate cart total: %w", err)
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cart items: %w", err)
	}
	return &result, nil
}

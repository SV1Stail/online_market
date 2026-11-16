package order

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/catalog"
	"github.com/SV1Stail/online_market/internal/identifier"
	"github.com/SV1Stail/online_market/internal/money"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Create(ctx context.Context, command CheckoutCommand) (*Order, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin checkout: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize attempts with the same idempotency key before checking the
	// unique row, otherwise two concurrent first requests could race to insert.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, command.IdempotencyKey); err != nil {
		return nil, fmt.Errorf("lock idempotency key: %w", err)
	}

	var existingOrderID string
	var existingHash []byte
	var existingCartTokenHash []byte
	err = tx.QueryRow(ctx, `
		SELECT o.id::text, o.checkout_request_hash, c.token_hash
		FROM orders o
		JOIN carts c ON c.id = o.cart_id
		WHERE o.checkout_idempotency_key = $1
	`, command.IdempotencyKey).Scan(&existingOrderID, &existingHash, &existingCartTokenHash)
	if err == nil {
		if !bytes.Equal(existingCartTokenHash, command.CartTokenHash) {
			return nil, apperror.ErrUnauthenticated
		}
		if !bytes.Equal(existingHash, command.RequestHash) {
			return nil, fmt.Errorf("%w: idempotency key was used with another request", apperror.ErrPrecondition)
		}
		result, err := loadOrder(ctx, tx, existingOrderID, "", nil)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit idempotent checkout: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("query idempotent order: %w", err)
	}

	var cartStatus string
	var cartCurrency string
	var cartExpiresAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT status, currency_code, expires_at
		FROM carts
		WHERE id = $1::uuid AND token_hash = $2
		FOR UPDATE
	`, command.CartID, command.CartTokenHash).Scan(&cartStatus, &cartCurrency, &cartExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.ErrUnauthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("lock checkout cart: %w", err)
	}
	if cartStatus != "open" {
		return nil, fmt.Errorf("%w: cart is not open", apperror.ErrPrecondition)
	}
	if !command.CreatedAt.Before(cartExpiresAt) {
		return nil, fmt.Errorf("%w: cart is expired", apperror.ErrPrecondition)
	}

	checkoutItems, err := loadCheckoutItems(ctx, tx, command.CartID, cartCurrency, command.CreatedAt)
	if err != nil {
		return nil, err
	}
	items := checkoutItems.Items
	total := checkoutItems.Total
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: cart is empty", apperror.ErrPrecondition)
	}

	orderName := "Заказ " + command.OrderNumber
	_, err = tx.Exec(ctx, `
		INSERT INTO orders (
			id, number, name, cart_id, order_token_hash,
			checkout_idempotency_key, checkout_request_hash,
			customer_full_name, customer_phone, customer_email,
			delivery_method, delivery_city, delivery_address, delivery_comment,
			customer_comment, status, total_minor_units, currency_code,
			version, created_at, updated_at
		) VALUES (
			$1::uuid, $2, $3, $4::uuid, $5,
			$6, $7,
			$8, $9, $10,
			$11, $12, $13, $14,
			$15, 'new', $16, $17,
			1, $18, $18
		)
	`,
		command.OrderID, command.OrderNumber, orderName, command.CartID, command.OrderTokenHash,
		command.IdempotencyKey, command.RequestHash,
		command.Customer.FullName, command.Customer.Phone, command.Customer.Email,
		command.Delivery.Method, command.Delivery.City, command.Delivery.Address, command.Delivery.Comment,
		command.CustomerComment, total.MinorUnits, total.Currency, command.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert order: %w", err)
	}
	for _, item := range items {
		_, err := tx.Exec(ctx, `
			INSERT INTO order_items (
				id, order_id, product_id, product_name,
				unit_price_minor_units, currency_code, quantity,
				line_total_minor_units, created_at, updated_at
			) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $9)
		`,
			item.ID, command.OrderID, item.ProductID, item.ProductName,
			item.UnitPrice.MinorUnits, item.UnitPrice.Currency, item.Quantity,
			item.LineTotal.MinorUnits, command.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("insert order item: %w", err)
		}
	}
	eventID, err := identifier.NewUUID()
	if err != nil {
		return nil, fmt.Errorf("generate order event id: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO order_events (id, order_id, event_type, comment, created_at)
		VALUES ($1::uuid, $2::uuid, 'created', '', $3)
	`, eventID, command.OrderID, command.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert order event: %w", err)
	}

	if err := enqueueCreatedNotifications(ctx, tx, command, total); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE carts
		SET status = 'checked_out', version = version + 1, updated_at = $2
		WHERE id = $1::uuid
	`, command.CartID, command.CreatedAt); err != nil {
		return nil, fmt.Errorf("mark cart checked out: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit checkout: %w", err)
	}

	return &Order{
		ID: command.OrderID, Number: command.OrderNumber, Name: orderName, CartID: command.CartID,
		Customer: command.Customer, Delivery: command.Delivery, CustomerComment: command.CustomerComment,
		Status: StatusNew, Items: items, Total: total, Version: 1,
		CreatedAt: command.CreatedAt, UpdatedAt: command.CreatedAt,
	}, nil
}

func (r *PostgresRepository) GetPublic(ctx context.Context, number string, tokenHash []byte) (*Order, error) {
	return loadOrder(ctx, r.pool, "", number, tokenHash)
}

type checkoutItemsResult struct {
	Items []Item
	Total money.Money
}

func loadCheckoutItems(ctx context.Context, tx pgx.Tx, cartID, currency string, now time.Time) (*checkoutItemsResult, error) {
	rows, err := tx.Query(ctx, `
		SELECT p.id::text, p.name, p.price_minor_units, p.currency_code, p.availability, ci.quantity
		FROM cart_items ci
		JOIN products p ON p.id = ci.product_id
		WHERE ci.cart_id = $1::uuid
		ORDER BY ci.created_at, ci.id
	`, cartID)
	if err != nil {
		return nil, fmt.Errorf("query checkout items: %w", err)
	}
	defer rows.Close()

	total, err := money.New(0, currency)
	if err != nil {
		return nil, fmt.Errorf("invalid cart currency: %w", err)
	}
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		var minorUnits int64
		var itemCurrency string
		var availability catalog.Availability
		if err := rows.Scan(&item.ProductID, &item.ProductName, &minorUnits, &itemCurrency, &availability, &item.Quantity); err != nil {
			return nil, fmt.Errorf("scan checkout item: %w", err)
		}
		if availability != catalog.AvailabilityAvailable {
			return nil, fmt.Errorf("%w: product %s is not available", apperror.ErrPrecondition, item.ProductID)
		}
		if itemCurrency != currency {
			return nil, fmt.Errorf("%w: product currency differs from cart currency", apperror.ErrPrecondition)
		}
		item.ID, err = identifier.NewUUID()
		if err != nil {
			return nil, fmt.Errorf("generate order item id: %w", err)
		}
		item.UnitPrice, err = money.New(minorUnits, itemCurrency)
		if err != nil {
			return nil, fmt.Errorf("invalid checkout price: %w", err)
		}
		item.LineTotal, err = item.UnitPrice.Multiply(item.Quantity)
		if err != nil {
			return nil, fmt.Errorf("calculate item total: %w", err)
		}
		total, err = total.Add(item.LineTotal)
		if err != nil {
			return nil, fmt.Errorf("calculate order total: %w", err)
		}
		item.CreatedAt = now
		item.UpdatedAt = now
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate checkout items: %w", err)
	}
	return &checkoutItemsResult{Items: items, Total: total}, nil
}

func enqueueCreatedNotifications(ctx context.Context, tx pgx.Tx, command CheckoutCommand, total money.Money) error {
	text := fmt.Sprintf(
		"Новый заказ %s\nПокупатель: %s\nТелефон: %s\nСумма: %d %s",
		command.OrderNumber,
		command.Customer.FullName,
		command.Customer.Phone,
		total.MinorUnits,
		total.Currency,
	)
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("marshal notification payload: %w", err)
	}
	for _, channel := range command.NotificationChannels {
		outboxID, err := identifier.NewUUID()
		if err != nil {
			return fmt.Errorf("generate outbox id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO notification_outbox (
				id, order_id, notification_type, channel,
				deduplication_key, payload, status, attempts,
				available_at, created_at, updated_at
			) VALUES ($1::uuid, $2::uuid, 'order_created', $3, $4, $5, 'pending', 0, $6, $6, $6)
	`, outboxID, command.OrderID, channel, "order_created:"+command.OrderID, string(payload), command.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert notification outbox: %w", err)
		}
	}
	return nil
}

type orderQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadOrder(ctx context.Context, query orderQuerier, orderID, number string, tokenHash []byte) (*Order, error) {
	var result Order
	var totalMinor int64
	var currency string
	var assignedAdminID sql.NullString
	row := query.QueryRow(ctx, `
		SELECT
			id::text, number, name, cart_id::text,
			customer_full_name, customer_phone, customer_email,
			delivery_method, delivery_city, delivery_address, delivery_comment,
			customer_comment, internal_comment, assigned_admin_id::text,
			status, total_minor_units, currency_code, version, created_at, updated_at
		FROM orders
		WHERE ($1::uuid IS NOT NULL AND id = $1::uuid)
			OR ($2::text <> '' AND number = $2 AND order_token_hash = $3)
	`, nullableUUID(orderID), number, tokenHash)
	err := row.Scan(
		&result.ID, &result.Number, &result.Name, &result.CartID,
		&result.Customer.FullName, &result.Customer.Phone, &result.Customer.Email,
		&result.Delivery.Method, &result.Delivery.City, &result.Delivery.Address, &result.Delivery.Comment,
		&result.CustomerComment, &result.InternalComment, &assignedAdminID,
		&result.Status, &totalMinor, &currency, &result.Version, &result.CreatedAt, &result.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if number != "" {
			return nil, apperror.ErrUnauthenticated
		}
		return nil, apperror.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan order: %w", err)
	}
	if assignedAdminID.Valid {
		result.AssignedAdminID = assignedAdminID.String
	}
	result.Total, err = money.New(totalMinor, currency)
	if err != nil {
		return nil, fmt.Errorf("invalid stored order total: %w", err)
	}

	rows, err := query.Query(ctx, `
		SELECT id::text, COALESCE(product_id::text, ''), product_name,
			unit_price_minor_units, currency_code, quantity,
			line_total_minor_units, created_at, updated_at
		FROM order_items
		WHERE order_id = $1::uuid
		ORDER BY created_at, id
	`, result.ID)
	if err != nil {
		return nil, fmt.Errorf("query order items: %w", err)
	}
	defer rows.Close()
	result.Items = make([]Item, 0)
	for rows.Next() {
		var item Item
		var unitMinor int64
		var lineMinor int64
		var itemCurrency string
		if err := rows.Scan(
			&item.ID, &item.ProductID, &item.ProductName,
			&unitMinor, &itemCurrency, &item.Quantity,
			&lineMinor, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		item.UnitPrice, err = money.New(unitMinor, itemCurrency)
		if err != nil {
			return nil, fmt.Errorf("invalid stored unit price: %w", err)
		}
		item.LineTotal, err = money.New(lineMinor, itemCurrency)
		if err != nil {
			return nil, fmt.Errorf("invalid stored line total: %w", err)
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order items: %w", err)
	}
	return &result, nil
}

func nullableUUID(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

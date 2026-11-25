package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/catalog"
	"github.com/SV1Stail/online_market/internal/identifier"
	"github.com/SV1Stail/online_market/internal/money"
	"github.com/SV1Stail/online_market/internal/pagination"
)

func (r *PostgresRepository) List(ctx context.Context, filter ListFilter) ([]Order, error) {
	statuses := make([]string, len(filter.Statuses))
	for index, status := range filter.Statuses {
		statuses[index] = string(status)
	}
	if statuses == nil {
		statuses = []string{}
	}
	var cursorTime any
	var cursorID any
	if filter.Cursor != nil {
		cursorTime = filter.Cursor.CreatedAt
		cursorID = filter.Cursor.ID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text
		FROM orders
		WHERE (cardinality($1::text[]) = 0 OR status = ANY($1::text[]))
			AND (
				$2::text = ''
				OR number ILIKE '%' || $2 || '%'
				OR name ILIKE '%' || $2 || '%'
				OR customer_full_name ILIKE '%' || $2 || '%'
				OR customer_phone ILIKE '%' || $2 || '%'
			)
			AND ($3::timestamptz IS NULL OR created_at >= $3)
			AND ($4::timestamptz IS NULL OR created_at < $4)
			AND ($5::text = '' OR assigned_admin_id = $5::uuid)
			AND ($6::timestamptz IS NULL OR (created_at, id) < ($6::timestamptz, $7::uuid))
		ORDER BY created_at DESC, id DESC
		LIMIT $8
	`, statuses, filter.Query, filter.CreatedFrom, filter.CreatedTo, filter.AssignedAdminID, cursorTime, cursorID, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("query admin order ids: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan admin order id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin order ids: %w", err)
	}

	orders := make([]Order, 0, len(ids))
	for _, id := range ids {
		current, err := loadOrder(ctx, r.pool, id, "", nil)
		if err != nil {
			return nil, err
		}
		orders = append(orders, *current)
	}
	return orders, nil
}

func (r *PostgresRepository) GetByID(ctx context.Context, orderID string) (*Order, error) {
	return loadOrder(ctx, r.pool, orderID, "", nil)
}

func (r *PostgresRepository) UpdateDetails(
	ctx context.Context,
	orderID string,
	expectedVersion uint64,
	actorID string,
	patch DetailsPatch,
	channels []string,
) (*Order, error) {
	mutation, err := r.startMutation(ctx, orderID, expectedVersion)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mutation.tx.Rollback(ctx) }()
	changes := make([]FieldChange, 0, len(patch.Paths))
	for _, path := range patch.Paths {
		switch path {
		case "name":
			_, err = mutation.tx.Exec(ctx, `UPDATE orders SET name = $2 WHERE id = $1::uuid`, orderID, strings.TrimSpace(patch.Name))
			changes = append(changes, change(path, mutation.current.Name, strings.TrimSpace(patch.Name)))
		case "customer":
			_, err = mutation.tx.Exec(ctx, `
				UPDATE orders
				SET customer_full_name = $2, customer_phone = $3, customer_email = $4
				WHERE id = $1::uuid
			`, orderID, strings.TrimSpace(patch.Customer.FullName), strings.TrimSpace(patch.Customer.Phone), strings.TrimSpace(patch.Customer.Email))
			changes = append(changes, change(path, customerString(mutation.current.Customer), customerString(patch.Customer)))
		case "delivery":
			_, err = mutation.tx.Exec(ctx, `
				UPDATE orders
				SET delivery_method = $2, delivery_city = $3, delivery_address = $4, delivery_comment = $5
				WHERE id = $1::uuid
			`, orderID, patch.Delivery.Method, strings.TrimSpace(patch.Delivery.City), strings.TrimSpace(patch.Delivery.Address), strings.TrimSpace(patch.Delivery.Comment))
			changes = append(changes, change(path, deliveryString(mutation.current.Delivery), deliveryString(patch.Delivery)))
		case "customer_comment":
			_, err = mutation.tx.Exec(ctx, `UPDATE orders SET customer_comment = $2 WHERE id = $1::uuid`, orderID, patch.CustomerComment)
			changes = append(changes, change(path, mutation.current.CustomerComment, patch.CustomerComment))
		case "internal_comment":
			_, err = mutation.tx.Exec(ctx, `UPDATE orders SET internal_comment = $2 WHERE id = $1::uuid`, orderID, patch.InternalComment)
			changes = append(changes, change(path, mutation.current.InternalComment, patch.InternalComment))
		case "assigned_admin_id":
			_, err = mutation.tx.Exec(ctx, `UPDATE orders SET assigned_admin_id = $2::uuid WHERE id = $1::uuid`, orderID, nullableUUID(patch.AssignedAdminID))
			changes = append(changes, change(path, mutation.current.AssignedAdminID, patch.AssignedAdminID))
		}
		if err != nil {
			return nil, fmt.Errorf("update order field %s: %w", path, err)
		}
	}
	return r.finishMutation(ctx, mutation, actorID, EventDetailsChanged, "", changes, channels)
}

func (r *PostgresRepository) AddItem(
	ctx context.Context,
	orderID string,
	expectedVersion uint64,
	actorID string,
	productID string,
	quantity uint32,
	priceOverride *money.Money,
	channels []string,
) (*Order, error) {
	mutation, err := r.startMutation(ctx, orderID, expectedVersion)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mutation.tx.Rollback(ctx) }()
	var productName string
	var minorUnits int64
	var currency string
	var availability catalog.Availability
	err = mutation.tx.QueryRow(ctx, `
		SELECT name, price_minor_units, currency_code, availability
		FROM products WHERE id = $1::uuid
	`, productID).Scan(&productName, &minorUnits, &currency, &availability)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query product for order: %w", err)
	}
	if availability == catalog.AvailabilityArchived {
		return nil, fmt.Errorf("%w: archived product cannot be added", apperror.ErrPrecondition)
	}
	unitPrice, err := money.New(minorUnits, currency)
	if err != nil {
		return nil, fmt.Errorf("invalid product price: %w", err)
	}
	if priceOverride != nil {
		unitPrice = *priceOverride
	}
	if unitPrice.Currency != mutation.current.Total.Currency {
		return nil, fmt.Errorf("%w: product currency differs from order currency", apperror.ErrPrecondition)
	}
	lineTotal, err := unitPrice.Multiply(quantity)
	if err != nil {
		return nil, err
	}
	itemID, err := identifier.NewUUID()
	if err != nil {
		return nil, fmt.Errorf("generate order item id: %w", err)
	}
	_, err = mutation.tx.Exec(ctx, `
		INSERT INTO order_items (
			id, order_id, product_id, product_name, unit_price_minor_units,
			currency_code, quantity, line_total_minor_units
		) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8)
	`, itemID, orderID, productID, productName, unitPrice.MinorUnits, unitPrice.Currency, quantity, lineTotal.MinorUnits)
	if err != nil {
		return nil, fmt.Errorf("insert admin order item: %w", err)
	}
	changes := []FieldChange{change("items."+itemID, "", productName+" x"+strconv.FormatUint(uint64(quantity), 10))}
	return r.finishMutation(ctx, mutation, actorID, EventItemAdded, "", changes, channels)
}

func (r *PostgresRepository) UpdateItem(
	ctx context.Context,
	orderID string,
	itemID string,
	expectedVersion uint64,
	actorID string,
	patch ItemPatch,
	channels []string,
) (*Order, error) {
	mutation, err := r.startMutation(ctx, orderID, expectedVersion)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mutation.tx.Rollback(ctx) }()
	var current Item
	var unitMinor int64
	var currency string
	err = mutation.tx.QueryRow(ctx, `
		SELECT id::text, COALESCE(product_id::text, ''), product_name,
			unit_price_minor_units, currency_code, quantity
		FROM order_items
		WHERE id = $1::uuid AND order_id = $2::uuid
		FOR UPDATE
	`, itemID, orderID).Scan(
		&current.ID, &current.ProductID, &current.ProductName,
		&unitMinor, &currency, &current.Quantity,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock order item: %w", err)
	}
	current.UnitPrice, err = money.New(unitMinor, currency)
	if err != nil {
		return nil, err
	}
	updated := current
	changes := make([]FieldChange, 0, len(patch.Paths))
	for _, path := range patch.Paths {
		switch path {
		case "product_name":
			updated.ProductName = strings.TrimSpace(patch.ProductName)
			changes = append(changes, change("items."+itemID+".product_name", current.ProductName, updated.ProductName))
		case "quantity":
			updated.Quantity = patch.Quantity
			changes = append(changes, change("items."+itemID+".quantity", strconv.FormatUint(uint64(current.Quantity), 10), strconv.FormatUint(uint64(updated.Quantity), 10)))
		case "unit_price":
			updated.UnitPrice = *patch.UnitPrice
			changes = append(changes, change("items."+itemID+".unit_price", moneyString(current.UnitPrice), moneyString(updated.UnitPrice)))
		}
	}
	updated.LineTotal, err = updated.UnitPrice.Multiply(updated.Quantity)
	if err != nil {
		return nil, err
	}
	_, err = mutation.tx.Exec(ctx, `
		UPDATE order_items
		SET product_name = $3, unit_price_minor_units = $4,
			quantity = $5, line_total_minor_units = $6, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1::uuid AND order_id = $2::uuid
	`, itemID, orderID, updated.ProductName, updated.UnitPrice.MinorUnits, updated.Quantity, updated.LineTotal.MinorUnits)
	if err != nil {
		return nil, fmt.Errorf("update order item: %w", err)
	}
	return r.finishMutation(ctx, mutation, actorID, EventItemChanged, "", changes, channels)
}

func (r *PostgresRepository) RemoveItem(
	ctx context.Context,
	orderID string,
	itemID string,
	expectedVersion uint64,
	actorID string,
	channels []string,
) (*Order, error) {
	mutation, err := r.startMutation(ctx, orderID, expectedVersion)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mutation.tx.Rollback(ctx) }()
	var count int
	if err := mutation.tx.QueryRow(ctx, `SELECT count(*) FROM order_items WHERE order_id = $1::uuid`, orderID).Scan(&count); err != nil {
		return nil, fmt.Errorf("count order items: %w", err)
	}
	if count <= 1 {
		return nil, fmt.Errorf("%w: order must contain at least one item", apperror.ErrPrecondition)
	}
	var productName string
	err = mutation.tx.QueryRow(ctx, `
		DELETE FROM order_items
		WHERE id = $1::uuid AND order_id = $2::uuid
		RETURNING product_name
	`, itemID, orderID).Scan(&productName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("delete order item: %w", err)
	}
	changes := []FieldChange{change("items."+itemID, productName, "")}
	return r.finishMutation(ctx, mutation, actorID, EventItemRemoved, "", changes, channels)
}

func (r *PostgresRepository) ChangeStatus(
	ctx context.Context,
	orderID string,
	expectedVersion uint64,
	actorID string,
	from Status,
	to Status,
	comment string,
	channels []string,
) (*Order, error) {
	mutation, err := r.startMutation(ctx, orderID, expectedVersion)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mutation.tx.Rollback(ctx) }()
	if mutation.current.Status != from || !CanTransition(mutation.current.Status, to) {
		return nil, apperror.ErrConflict
	}
	if _, err := mutation.tx.Exec(ctx, `UPDATE orders SET status = $2 WHERE id = $1::uuid`, orderID, to); err != nil {
		return nil, fmt.Errorf("update order status: %w", err)
	}
	changes := []FieldChange{change("status", string(from), string(to))}
	return r.finishMutation(ctx, mutation, actorID, EventStatusChanged, comment, changes, channels)
}

func (r *PostgresRepository) ListEvents(ctx context.Context, orderID string, cursor *pagination.Cursor, limit int) ([]Event, error) {
	var cursorTime any
	var cursorID any
	if cursor != nil {
		cursorTime = cursor.CreatedAt
		cursorID = cursor.ID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, order_id::text, event_type,
			COALESCE(actor_admin_id::text, ''), comment, created_at
		FROM order_events
		WHERE order_id = $1::uuid
			AND ($2::timestamptz IS NULL OR (created_at, id) < ($2::timestamptz, $3::uuid))
		ORDER BY created_at DESC, id DESC
		LIMIT $4
	`, orderID, cursorTime, cursorID, limit)
	if err != nil {
		return nil, fmt.Errorf("query order events: %w", err)
	}
	defer rows.Close()
	events := make([]Event, 0)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.OrderID, &event.Type, &event.ActorAdminID, &event.Comment, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan order event: %w", err)
		}
		changeRows, err := r.pool.Query(ctx, `
			SELECT field_path, old_value, new_value
			FROM order_event_changes
			WHERE order_event_id = $1::uuid
			ORDER BY id
		`, event.ID)
		if err != nil {
			return nil, fmt.Errorf("query event changes: %w", err)
		}
		for changeRows.Next() {
			var current FieldChange
			if err := changeRows.Scan(&current.FieldPath, &current.OldValue, &current.NewValue); err != nil {
				changeRows.Close()
				return nil, fmt.Errorf("scan event change: %w", err)
			}
			event.Changes = append(event.Changes, current)
		}
		if err := changeRows.Err(); err != nil {
			changeRows.Close()
			return nil, fmt.Errorf("iterate event changes: %w", err)
		}
		changeRows.Close()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order events: %w", err)
	}
	return events, nil
}

type mutationContext struct {
	tx      pgx.Tx
	current *Order
}

func (r *PostgresRepository) startMutation(ctx context.Context, orderID string, expectedVersion uint64) (*mutationContext, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin order mutation: %w", err)
	}
	var version uint64
	err = tx.QueryRow(ctx, `SELECT version FROM orders WHERE id = $1::uuid FOR UPDATE`, orderID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return nil, apperror.ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("lock order: %w", err)
	}
	if version != expectedVersion {
		_ = tx.Rollback(ctx)
		return nil, apperror.ErrConflict
	}
	current, err := loadOrder(ctx, tx, orderID, "", nil)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	if current.Status == StatusCompleted || current.Status == StatusCancelled {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("%w: terminal order cannot be edited", apperror.ErrPrecondition)
	}
	return &mutationContext{tx: tx, current: current}, nil
}

func (r *PostgresRepository) finishMutation(
	ctx context.Context,
	mutation *mutationContext,
	actorID string,
	eventType EventType,
	comment string,
	changes []FieldChange,
	channels []string,
) (*Order, error) {
	var total int64
	if err := mutation.tx.QueryRow(ctx, `
		SELECT COALESCE(sum(line_total_minor_units), 0)
		FROM order_items WHERE order_id = $1::uuid
	`, mutation.current.ID).Scan(&total); err != nil {
		return nil, fmt.Errorf("recalculate order total: %w", err)
	}
	if _, err := mutation.tx.Exec(ctx, `
		UPDATE orders
		SET total_minor_units = $2, version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1::uuid
	`, mutation.current.ID, total); err != nil {
		return nil, fmt.Errorf("finalize order mutation: %w", err)
	}
	eventID, err := identifier.NewUUID()
	if err != nil {
		return nil, fmt.Errorf("generate audit event id: %w", err)
	}
	if _, err := mutation.tx.Exec(ctx, `
		INSERT INTO order_events (id, order_id, event_type, actor_admin_id, comment)
		VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5)
	`, eventID, mutation.current.ID, eventType, actorID, comment); err != nil {
		return nil, fmt.Errorf("insert audit event: %w", err)
	}
	for _, current := range changes {
		if _, err := mutation.tx.Exec(ctx, `
			INSERT INTO order_event_changes (order_event_id, field_path, old_value, new_value)
			VALUES ($1::uuid, $2, $3, $4)
		`, eventID, current.FieldPath, current.OldValue, current.NewValue); err != nil {
			return nil, fmt.Errorf("insert audit field change: %w", err)
		}
	}
	updated, err := loadOrder(ctx, mutation.tx, mutation.current.ID, "", nil)
	if err != nil {
		return nil, err
	}
	if err := enqueueUpdatedNotifications(ctx, mutation.tx, updated, eventID, eventType, channels); err != nil {
		return nil, err
	}
	if err := mutation.tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit order mutation: %w", err)
	}
	return updated, nil
}

func enqueueUpdatedNotifications(ctx context.Context, tx pgx.Tx, updated *Order, eventID string, eventType EventType, channels []string) error {
	text := fmt.Sprintf("Заказ %s изменён\nСтатус: %s\nСумма: %d %s", updated.Number, updated.Status, updated.Total.MinorUnits, updated.Total.Currency)
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("marshal order update notification: %w", err)
	}
	for _, channel := range channels {
		outboxID, err := identifier.NewUUID()
		if err != nil {
			return fmt.Errorf("generate update outbox id: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO notification_outbox (
				id, order_id, notification_type, channel,
				deduplication_key, payload
			) VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)
		`, outboxID, updated.ID, string(eventType), channel, "order_event:"+eventID, string(payload))
		if err != nil {
			return fmt.Errorf("insert update outbox: %w", err)
		}
	}
	return nil
}

func change(path, oldValue, newValue string) FieldChange {
	return FieldChange{FieldPath: path, OldValue: oldValue, NewValue: newValue}
}

func customerString(value Customer) string {
	return strings.Join([]string{value.FullName, value.Phone, value.Email}, " | ")
}

func deliveryString(value Delivery) string {
	return strings.Join([]string{string(value.Method), value.City, value.Address, value.Comment}, " | ")
}

func moneyString(value money.Money) string {
	return strconv.FormatInt(value.MinorUnits, 10) + " " + value.Currency
}

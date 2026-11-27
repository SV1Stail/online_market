package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/identifier"
)

type placeQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (r *PostgresRepository) ListDeliveryPlaces(ctx context.Context) ([]DeliveryPlace, error) {
	places, err := loadDeliveryPlaces(ctx, r.pool, "")
	if err != nil {
		return nil, err
	}
	return places, nil
}

func (r *PostgresRepository) CreateDeliveryPlace(ctx context.Context, place DeliveryPlace) (DeliveryPlace, error) {
	id, err := identifier.NewUUID()
	if err != nil {
		return DeliveryPlace{}, fmt.Errorf("generate delivery place id: %w", err)
	}
	place.ID = id
	return r.saveDeliveryPlace(ctx, place, false)
}

func (r *PostgresRepository) UpdateDeliveryPlace(ctx context.Context, place DeliveryPlace) (DeliveryPlace, error) {
	return r.saveDeliveryPlace(ctx, place, true)
}

func (r *PostgresRepository) DeleteDeliveryPlace(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM delivery_places WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("delete delivery place: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) saveDeliveryPlace(ctx context.Context, place DeliveryPlace, update bool) (DeliveryPlace, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return DeliveryPlace{}, fmt.Errorf("begin delivery place: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	key := PlaceKey(place.Name)
	if update {
		tag, err := tx.Exec(ctx, `
			UPDATE delivery_places
			SET name = $2, name_key = $3, daily_limit = $4, schedule_kind = $5, updated_at = CURRENT_TIMESTAMP
			WHERE id = $1::uuid
		`, place.ID, place.Name, key, place.DailyLimit, place.ScheduleKind)
		if err != nil {
			if isUniqueViolation(err) {
				return DeliveryPlace{}, apperror.Validation("name", "already exists")
			}
			return DeliveryPlace{}, fmt.Errorf("update delivery place: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return DeliveryPlace{}, apperror.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM delivery_place_dates WHERE place_id = $1::uuid`, place.ID); err != nil {
			return DeliveryPlace{}, fmt.Errorf("clear delivery place dates: %w", err)
		}
	} else {
		_, err := tx.Exec(ctx, `
			INSERT INTO delivery_places (id, name, name_key, daily_limit, schedule_kind)
			VALUES ($1::uuid, $2, $3, $4, $5)
		`, place.ID, place.Name, key, place.DailyLimit, place.ScheduleKind)
		if err != nil {
			if isUniqueViolation(err) {
				return DeliveryPlace{}, apperror.Validation("name", "already exists")
			}
			return DeliveryPlace{}, fmt.Errorf("insert delivery place: %w", err)
		}
	}
	if place.ScheduleKind == ScheduleDates {
		for _, date := range place.Dates {
			if _, err := tx.Exec(ctx, `
				INSERT INTO delivery_place_dates (place_id, delivery_date)
				VALUES ($1::uuid, $2::date)
			`, place.ID, FormatDeliveryDate(date)); err != nil {
				return DeliveryPlace{}, fmt.Errorf("insert delivery place date: %w", err)
			}
		}
	} else {
		place.Dates = nil
	}
	if err := tx.Commit(ctx); err != nil {
		return DeliveryPlace{}, fmt.Errorf("commit delivery place: %w", err)
	}
	return place, nil
}

func (r *PostgresRepository) reserveDeliverySlot(ctx context.Context, tx pgx.Tx, city string, date time.Time) error {
	key := PlaceKey(city)
	day := FormatDeliveryDate(date)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "delivery-place:"+key+":"+day); err != nil {
		return fmt.Errorf("lock delivery place: %w", err)
	}
	place, err := loadDeliveryPlace(ctx, tx, key)
	if err != nil {
		return err
	}
	if !PlaceServes(place.ScheduleKind, date, place.Dates) {
		return apperror.Validation("delivery.date", DeliveryDateUnavailableMessage)
	}
	var booked int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM orders
		WHERE delivery_date = $1::date
			AND lower(btrim(delivery_city)) = $2
			AND status <> 'cancelled'
	`, day, key).Scan(&booked); err != nil {
		return fmt.Errorf("count delivery place orders: %w", err)
	}
	if booked >= place.DailyLimit {
		return deliveryDayFullError()
	}
	return nil
}

func (r *PostgresRepository) ListDeliveryDays(ctx context.Context, city string, from time.Time, days int) ([]DeliveryDay, error) {
	place, err := loadDeliveryPlace(ctx, r.pool, PlaceKey(city))
	if err != nil {
		return nil, err
	}
	dates := ScheduledDates(place.ScheduleKind, from, days, place.Dates)
	booked, err := countPlaceBookings(ctx, r.pool, PlaceKey(place.Name), from, from.AddDate(0, 0, days-1))
	if err != nil {
		return nil, err
	}
	result := make([]DeliveryDay, 0, len(dates))
	for _, date := range dates {
		used := booked[FormatDeliveryDate(date)]
		result = append(result, DeliveryDay{Date: date, Booked: used, Limit: place.DailyLimit})
	}
	return result, nil
}

func loadDeliveryPlace(ctx context.Context, query placeQuerier, key string) (DeliveryPlace, error) {
	places, err := loadDeliveryPlaces(ctx, query, key)
	if err != nil {
		return DeliveryPlace{}, err
	}
	if len(places) == 0 {
		return DeliveryPlace{}, apperror.Validation("delivery.city", DeliveryCityUnavailableMessage)
	}
	return places[0], nil
}

func loadDeliveryPlaces(ctx context.Context, query placeQuerier, key string) ([]DeliveryPlace, error) {
	queryText := `
		SELECT id::text, name, daily_limit, schedule_kind
		FROM delivery_places
	`
	args := []any{}
	if key != "" {
		queryText += ` WHERE name_key = $1`
		args = append(args, key)
	}
	queryText += ` ORDER BY name`
	rows, err := query.Query(ctx, queryText, args...)
	if err != nil {
		return nil, fmt.Errorf("query delivery places: %w", err)
	}
	defer rows.Close()
	places := make([]DeliveryPlace, 0)
	known := make(map[string]struct{})
	for rows.Next() {
		var place DeliveryPlace
		if err := rows.Scan(&place.ID, &place.Name, &place.DailyLimit, &place.ScheduleKind); err != nil {
			return nil, fmt.Errorf("scan delivery place: %w", err)
		}
		places = append(places, place)
		known[place.ID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delivery places: %w", err)
	}
	if len(places) == 0 {
		return places, nil
	}
	dateRows, err := query.Query(ctx, `
		SELECT place_id::text, delivery_date
		FROM delivery_place_dates
		ORDER BY delivery_date
	`)
	if err != nil {
		return nil, fmt.Errorf("query delivery place dates: %w", err)
	}
	defer dateRows.Close()
	byID := make(map[string][]time.Time, len(places))
	for dateRows.Next() {
		var id string
		var stored time.Time
		if err := dateRows.Scan(&id, &stored); err != nil {
			return nil, fmt.Errorf("scan delivery place date: %w", err)
		}
		if _, ok := known[id]; !ok {
			continue
		}
		normalized := time.Date(stored.UTC().Year(), stored.UTC().Month(), stored.UTC().Day(), 0, 0, 0, 0, time.UTC)
		byID[id] = append(byID[id], normalized)
	}
	if err := dateRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delivery place dates: %w", err)
	}
	for index := range places {
		places[index].Dates = byID[places[index].ID]
	}
	return places, nil
}

func countPlaceBookings(ctx context.Context, query placeQuerier, key string, from, to time.Time) (map[string]int, error) {
	rows, err := query.Query(ctx, `
		SELECT delivery_date, COUNT(*)
		FROM orders
		WHERE status <> 'cancelled'
			AND lower(btrim(delivery_city)) = $1
			AND delivery_date >= $2::date
			AND delivery_date <= $3::date
		GROUP BY delivery_date
	`, key, FormatDeliveryDate(from), FormatDeliveryDate(to))
	if err != nil {
		return nil, fmt.Errorf("count delivery place bookings: %w", err)
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var stored time.Time
		var booked int
		if err := rows.Scan(&stored, &booked); err != nil {
			return nil, fmt.Errorf("scan delivery place booking: %w", err)
		}
		normalized := time.Date(stored.UTC().Year(), stored.UTC().Month(), stored.UTC().Day(), 0, 0, 0, 0, time.UTC)
		result[FormatDeliveryDate(normalized)] = booked
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delivery place bookings: %w", err)
	}
	return result, nil
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

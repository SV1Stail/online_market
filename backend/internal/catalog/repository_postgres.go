package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/money"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) ListProducts(ctx context.Context, filter ListFilter) ([]Product, error) {
	if filter.CategoryIDs == nil {
		filter.CategoryIDs = []string{}
	}
	var cursorTime any
	var cursorID any
	if filter.Cursor != nil {
		cursorTime = filter.Cursor.CreatedAt
		cursorID = filter.Cursor.ID
	}

	rows, err := r.pool.Query(ctx, `
		SELECT
			p.id::text,
			p.name,
			p.description,
			p.price_minor_units,
			p.currency_code,
			p.availability,
			p.version,
			p.created_at,
			p.updated_at,
			COALESCE(
				array_agg(pc.category_id::text ORDER BY pc.category_id)
					FILTER (WHERE pc.category_id IS NOT NULL),
				ARRAY[]::text[]
			)
		FROM products p
		LEFT JOIN product_categories pc ON pc.product_id = p.id
		WHERE p.availability <> 'archived'
			AND ($1::text = '' OR p.name ILIKE '%' || $1 || '%' OR p.description ILIKE '%' || $1 || '%')
			AND (NOT $2::boolean OR p.availability = 'available')
			AND (
				cardinality($3::text[]) = 0
				OR EXISTS (
					SELECT 1
					FROM product_categories selected_pc
					WHERE selected_pc.product_id = p.id
						AND selected_pc.category_id::text = ANY($3::text[])
				)
			)
			AND (
				$4::timestamptz IS NULL
				OR (p.created_at, p.id) < ($4::timestamptz, $5::uuid)
			)
		GROUP BY p.id
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $6
	`, filter.Query, filter.OnlyAvailable, filter.CategoryIDs, cursorTime, cursorID, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("query products: %w", err)
	}
	defer rows.Close()

	products := make([]Product, 0)
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, *product)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate products: %w", err)
	}
	return products, nil
}

func (r *PostgresRepository) GetProduct(ctx context.Context, productID string) (*Product, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			p.id::text,
			p.name,
			p.description,
			p.price_minor_units,
			p.currency_code,
			p.availability,
			p.version,
			p.created_at,
			p.updated_at,
			COALESCE(
				array_agg(pc.category_id::text ORDER BY pc.category_id)
					FILTER (WHERE pc.category_id IS NOT NULL),
				ARRAY[]::text[]
			)
		FROM products p
		LEFT JOIN product_categories pc ON pc.product_id = p.id
		WHERE p.id = $1::uuid AND p.availability <> 'archived'
		GROUP BY p.id
	`, productID)

	product, err := scanProduct(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return product, nil
}

func (r *PostgresRepository) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, name, parent_id::text, sort_order, version, created_at, updated_at
		FROM categories
		ORDER BY parent_id NULLS FIRST, sort_order, id
	`)
	if err != nil {
		return nil, fmt.Errorf("query categories: %w", err)
	}
	defer rows.Close()

	categories := make([]Category, 0)
	for rows.Next() {
		category, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		categories = append(categories, *category)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories: %w", err)
	}
	return categories, nil
}

func scanCategory(scanner productScanner) (*Category, error) {
	var category Category
	var parentID sql.NullString
	if err := scanner.Scan(
		&category.ID,
		&category.Name,
		&parentID,
		&category.SortOrder,
		&category.Version,
		&category.CreatedAt,
		&category.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan category: %w", err)
	}
	if parentID.Valid {
		category.ParentID = parentID.String
	}
	return &category, nil
}

type productScanner interface {
	Scan(...any) error
}

func scanProduct(scanner productScanner) (*Product, error) {
	var product Product
	var minorUnits int64
	var currency string
	if err := scanner.Scan(
		&product.ID,
		&product.Name,
		&product.Description,
		&minorUnits,
		&currency,
		&product.Availability,
		&product.Version,
		&product.CreatedAt,
		&product.UpdatedAt,
		&product.CategoryIDs,
	); err != nil {
		return nil, fmt.Errorf("scan product: %w", err)
	}
	price, err := money.New(minorUnits, currency)
	if err != nil {
		return nil, fmt.Errorf("invalid stored product price: %w", err)
	}
	product.Price = price
	return &product, nil
}

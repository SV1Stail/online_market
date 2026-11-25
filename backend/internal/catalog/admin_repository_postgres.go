package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/SV1Stail/online_market/internal/apperror"
)

func (r *PostgresRepository) ListAdminProducts(ctx context.Context, filter AdminListFilter) ([]Product, error) {
	if filter.CategoryIDs == nil {
		filter.CategoryIDs = []string{}
	}
	availabilities := make([]string, len(filter.Availabilities))
	for index, availability := range filter.Availabilities {
		availabilities[index] = string(availability)
	}
	var cursorTime any
	var cursorID any
	if filter.Cursor != nil {
		cursorTime = filter.Cursor.CreatedAt
		cursorID = filter.Cursor.ID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
			p.id::text, p.name, p.description, p.price_minor_units,
			p.currency_code, p.availability, p.version, p.created_at, p.updated_at,
			COALESCE(
				array_agg(pc.category_id::text ORDER BY pc.category_id)
					FILTER (WHERE pc.category_id IS NOT NULL),
				ARRAY[]::text[]
			)
		FROM products p
		LEFT JOIN product_categories pc ON pc.product_id = p.id
		WHERE ($1::text = '' OR p.name ILIKE '%' || $1 || '%' OR p.description ILIKE '%' || $1 || '%')
			AND (
				cardinality($2::text[]) = 0
				OR EXISTS (
					SELECT 1 FROM product_categories selected_pc
					WHERE selected_pc.product_id = p.id
						AND selected_pc.category_id::text = ANY($2::text[])
				)
			)
			AND (cardinality($3::text[]) = 0 OR p.availability::text = ANY($3::text[]))
			AND (
				$4::timestamptz IS NULL
				OR (p.created_at, p.id) < ($4::timestamptz, $5::uuid)
			)
		GROUP BY p.id
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $6
	`, filter.Query, filter.CategoryIDs, availabilities, cursorTime, cursorID, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("query admin products: %w", err)
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
		return nil, fmt.Errorf("iterate admin products: %w", err)
	}
	return products, nil
}

func (r *PostgresRepository) GetAdminProduct(ctx context.Context, productID string) (*Product, error) {
	return loadAdminProduct(ctx, r.pool, productID)
}

func (r *PostgresRepository) CreateProduct(ctx context.Context, product Product) (*Product, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin product creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		INSERT INTO products (
			id, name, description, price_minor_units, currency_code, availability, version
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, 1)
	`, product.ID, product.Name, product.Description, product.Price.MinorUnits, product.Price.Currency, product.Availability)
	if err != nil {
		return nil, fmt.Errorf("insert product: %w", err)
	}
	if err := insertProductCategories(ctx, tx, product.ID, product.CategoryIDs); err != nil {
		return nil, err
	}
	result, err := loadAdminProduct(ctx, tx, product.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit product creation: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) UpdateProduct(ctx context.Context, productID string, version uint64, patch ProductPatch) (*Product, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin product update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sets := make([]string, 0, len(patch.Paths)+2)
	arguments := []any{productID, version}
	for _, path := range patch.Paths {
		switch path {
		case "name":
			arguments = append(arguments, patch.Name)
			sets = append(sets, fmt.Sprintf("name = $%d", len(arguments)))
		case "description":
			arguments = append(arguments, patch.Description)
			sets = append(sets, fmt.Sprintf("description = $%d", len(arguments)))
		case "price":
			arguments = append(arguments, patch.Price.MinorUnits, patch.Price.Currency)
			sets = append(sets,
				fmt.Sprintf("price_minor_units = $%d", len(arguments)-1),
				fmt.Sprintf("currency_code = $%d", len(arguments)),
			)
		case "availability":
			arguments = append(arguments, patch.Availability)
			sets = append(sets, fmt.Sprintf("availability = $%d", len(arguments)))
		case "category_ids":
		}
	}
	sets = append(sets, "version = version + 1", "updated_at = CURRENT_TIMESTAMP")
	tag, err := tx.Exec(ctx, `
		UPDATE products SET `+strings.Join(sets, ", ")+`
		WHERE id = $1::uuid AND version = $2
	`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("update product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, apperror.ErrConflict
	}
	if containsPath(patch.Paths, "category_ids") {
		if _, err := tx.Exec(ctx, `DELETE FROM product_categories WHERE product_id = $1::uuid`, productID); err != nil {
			return nil, fmt.Errorf("clear product categories: %w", err)
		}
		if err := insertProductCategories(ctx, tx, productID, patch.CategoryIDs); err != nil {
			return nil, err
		}
	}
	result, err := loadAdminProduct(ctx, tx, productID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit product update: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) ArchiveProduct(ctx context.Context, productID string, version uint64) (*Product, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE products
		SET availability = 'archived', version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1::uuid AND version = $2
	`, productID, version)
	if err != nil {
		return nil, fmt.Errorf("archive product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, apperror.ErrConflict
	}
	return loadAdminProduct(ctx, r.pool, productID)
}

func (r *PostgresRepository) GetCategory(ctx context.Context, categoryID string) (*Category, error) {
	return loadCategory(ctx, r.pool, categoryID)
}

func (r *PostgresRepository) CreateCategory(ctx context.Context, category Category) (*Category, error) {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO categories (id, name, parent_id, sort_order, version)
		VALUES ($1::uuid, $2, $3::uuid, $4, 1)
	`, category.ID, category.Name, nullableText(category.ParentID), category.SortOrder)
	if isForeignKeyViolation(err) {
		return nil, fmt.Errorf("%w: parent category does not exist", apperror.ErrPrecondition)
	}
	if err != nil {
		return nil, fmt.Errorf("insert category: %w", err)
	}
	return loadCategory(ctx, r.pool, category.ID)
}

func (r *PostgresRepository) UpdateCategory(ctx context.Context, categoryID string, version uint64, patch CategoryPatch) (*Category, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin category update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if containsPath(patch.Paths, "parent_id") && patch.ParentID != "" {
		var createsCycle bool
		err := tx.QueryRow(ctx, `
			WITH RECURSIVE descendants AS (
				SELECT id FROM categories WHERE parent_id = $1::uuid
				UNION
				SELECT c.id FROM categories c JOIN descendants d ON c.parent_id = d.id
			)
			SELECT EXISTS(SELECT 1 FROM descendants WHERE id = $2::uuid)
		`, categoryID, patch.ParentID).Scan(&createsCycle)
		if err != nil {
			return nil, fmt.Errorf("check category hierarchy: %w", err)
		}
		if createsCycle {
			return nil, fmt.Errorf("%w: category hierarchy must not contain cycles", apperror.ErrPrecondition)
		}
	}
	sets := make([]string, 0, len(patch.Paths)+2)
	arguments := []any{categoryID, version}
	for _, path := range patch.Paths {
		switch path {
		case "name":
			arguments = append(arguments, patch.Name)
			sets = append(sets, fmt.Sprintf("name = $%d", len(arguments)))
		case "parent_id":
			arguments = append(arguments, nullableText(patch.ParentID))
			sets = append(sets, fmt.Sprintf("parent_id = $%d::uuid", len(arguments)))
		case "sort_order":
			arguments = append(arguments, patch.SortOrder)
			sets = append(sets, fmt.Sprintf("sort_order = $%d", len(arguments)))
		}
	}
	sets = append(sets, "version = version + 1", "updated_at = CURRENT_TIMESTAMP")
	tag, err := tx.Exec(ctx, `
		UPDATE categories SET `+strings.Join(sets, ", ")+`
		WHERE id = $1::uuid AND version = $2
	`, arguments...)
	if isForeignKeyViolation(err) {
		return nil, fmt.Errorf("%w: parent category does not exist", apperror.ErrPrecondition)
	}
	if err != nil {
		return nil, fmt.Errorf("update category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, apperror.ErrConflict
	}
	result, err := loadCategory(ctx, tx, categoryID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit category update: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) DeleteCategory(ctx context.Context, categoryID string, version uint64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1::uuid AND version = $2`, categoryID, version)
	if isForeignKeyViolation(err) {
		return fmt.Errorf("%w: category is still in use", apperror.ErrPrecondition)
	}
	if err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.ErrConflict
	}
	return nil
}

type catalogQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadAdminProduct(ctx context.Context, query catalogQuerier, productID string) (*Product, error) {
	row := query.QueryRow(ctx, `
		SELECT
			p.id::text, p.name, p.description, p.price_minor_units,
			p.currency_code, p.availability, p.version, p.created_at, p.updated_at,
			COALESCE(
				array_agg(pc.category_id::text ORDER BY pc.category_id)
					FILTER (WHERE pc.category_id IS NOT NULL),
				ARRAY[]::text[]
			)
		FROM products p
		LEFT JOIN product_categories pc ON pc.product_id = p.id
		WHERE p.id = $1::uuid
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

func loadCategory(ctx context.Context, query catalogQuerier, categoryID string) (*Category, error) {
	category, err := scanCategory(query.QueryRow(ctx, `
		SELECT id::text, name, parent_id::text, sort_order, version, created_at, updated_at
		FROM categories WHERE id = $1::uuid
	`, categoryID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return category, nil
}

func insertProductCategories(ctx context.Context, tx pgx.Tx, productID string, categoryIDs []string) error {
	for _, categoryID := range categoryIDs {
		_, err := tx.Exec(ctx, `
			INSERT INTO product_categories (product_id, category_id)
			VALUES ($1::uuid, $2::uuid)
		`, productID, categoryID)
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: category does not exist", apperror.ErrPrecondition)
		}
		if err != nil {
			return fmt.Errorf("insert product category: %w", err)
		}
	}
	return nil
}

func nullableText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func isForeignKeyViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23503"
}

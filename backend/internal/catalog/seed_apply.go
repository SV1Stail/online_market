package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// catalogSeedLockKey serializes bootstrap across API replicas.
// The lock is held only until the transaction commits.
const catalogSeedLockKey int64 = 724163001

// SeedResult reports whether a clean catalog was filled.
type SeedResult struct {
	Applied    bool
	Categories int
	Products   int
}

// ApplyBootstrap inserts the document when both catalog tables are empty.
// A non-empty catalog is left unchanged.
func (r *PostgresRepository) ApplyBootstrap(ctx context.Context, doc *SeedDocument) (*SeedResult, error) {
	if doc == nil {
		return nil, fmt.Errorf("catalog seed document is required")
	}
	if err := doc.validateCreates(); err != nil {
		return nil, err
	}
	ordered, err := orderSeedCategories(doc.Categories)
	if err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin catalog seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, catalogSeedLockKey); err != nil {
		return nil, fmt.Errorf("lock catalog seed: %w", err)
	}
	var categoryCount int64
	var productCount int64
	if err := tx.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM categories), (SELECT COUNT(*) FROM products)
	`).Scan(&categoryCount, &productCount); err != nil {
		return nil, fmt.Errorf("count catalog rows: %w", err)
	}
	if categoryCount > 0 || productCount > 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit catalog seed skip: %w", err)
		}
		return &SeedResult{}, nil
	}
	for _, category := range ordered {
		row := category.stored()
		if _, err := tx.Exec(ctx, `
			INSERT INTO categories (id, name, parent_id, sort_order, version)
			VALUES ($1::uuid, $2, $3::uuid, $4, 1)
		`, row.ID, row.Name, nullableText(row.ParentID), row.SortOrder); err != nil {
			return nil, fmt.Errorf("insert seed category: %w", err)
		}
	}
	for _, product := range doc.Products {
		row := product.stored()
		if _, err := tx.Exec(ctx, `
			INSERT INTO products (
				id, name, description, price_minor_units, currency_code, availability, version,
				unit_of_measure, min_order_quantity, min_order_amount_minor_units
			) VALUES ($1::uuid, $2, $3, $4, $5, $6, 1, $7, $8, $9)
		`, row.ID, row.Name, row.Description, row.Price.MinorUnits, row.Price.Currency, row.Availability,
			row.UnitOfMeasure, row.MinOrderQuantity, nullableMinorUnits(row.MinOrderAmount)); err != nil {
			return nil, fmt.Errorf("insert seed product: %w", err)
		}
		if err := insertProductCategories(ctx, tx, row.ID, row.CategoryIDs); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit catalog seed: %w", err)
	}
	return &SeedResult{Applied: true, Categories: len(doc.Categories), Products: len(doc.Products)}, nil
}

// ImportResult counts rows created or changed by an administrator import.
// A file entry that matches an existing id and lists no fields is not counted.
type ImportResult struct {
	CreatedCategories int
	UpdatedCategories int
	CreatedProducts   int
	UpdatedProducts   int
}

// ApplyImport inserts new catalog rows and updates only fields present in the file.
// Rows that are not in the file stay unchanged. The whole change is one transaction.
func (r *PostgresRepository) ApplyImport(ctx context.Context, doc *SeedDocument) (*ImportResult, error) {
	if doc == nil {
		return nil, fmt.Errorf("catalog import document is required")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin catalog import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, catalogSeedLockKey); err != nil {
		return nil, fmt.Errorf("lock catalog import: %w", err)
	}
	existingParents, err := loadCategoryParents(ctx, tx)
	if err != nil {
		return nil, err
	}
	existingProducts, err := loadProductIDs(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := doc.missingNewFields(existingParents, existingProducts); err != nil {
		return nil, err
	}
	if err := mergedCategoryCycle(doc.Categories, existingParents); err != nil {
		return nil, err
	}
	ordered, err := orderSeedCategories(doc.Categories)
	if err != nil {
		return nil, err
	}
	result := &ImportResult{}
	for _, category := range ordered {
		if _, exists := existingParents[category.ID]; exists {
			continue
		}
		row := category.stored()
		if _, err := tx.Exec(ctx, `
			INSERT INTO categories (id, name, parent_id, sort_order, version)
			VALUES ($1::uuid, $2, $3::uuid, $4, 1)
		`, row.ID, row.Name, nullableText(row.ParentID), row.SortOrder); err != nil {
			return nil, fmt.Errorf("insert imported category: %w", err)
		}
		result.CreatedCategories++
	}
	for _, category := range doc.Categories {
		if _, exists := existingParents[category.ID]; !exists || len(category.Patch.Paths) == 0 {
			continue
		}
		if err := updateImportedCategory(ctx, tx, category); err != nil {
			return nil, err
		}
		result.UpdatedCategories++
	}
	for _, product := range doc.Products {
		if _, exists := existingProducts[product.ID]; exists {
			if len(product.Patch.Paths) == 0 {
				continue
			}
			if err := updateImportedProduct(ctx, tx, product); err != nil {
				return nil, err
			}
			result.UpdatedProducts++
			continue
		}
		row := product.stored()
		if _, err := tx.Exec(ctx, `
			INSERT INTO products (
				id, name, description, price_minor_units, currency_code, availability, version,
				unit_of_measure, min_order_quantity, min_order_amount_minor_units
			) VALUES ($1::uuid, $2, $3, $4, $5, $6, 1, $7, $8, $9)
		`, row.ID, row.Name, row.Description, row.Price.MinorUnits, row.Price.Currency, row.Availability,
			row.UnitOfMeasure, row.MinOrderQuantity, nullableMinorUnits(row.MinOrderAmount)); err != nil {
			return nil, fmt.Errorf("insert imported product: %w", err)
		}
		if err := insertProductCategories(ctx, tx, row.ID, row.CategoryIDs); err != nil {
			return nil, err
		}
		result.CreatedProducts++
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit catalog import: %w", err)
	}
	return result, nil
}

func (doc *SeedDocument) validateCreates() error {
	return doc.missingNewFields(nil, nil)
}

func (doc *SeedDocument) missingNewFields(existingCategories map[string]string, existingProducts map[string]struct{}) error {
	var errs []error
	for index, category := range doc.Categories {
		if existingCategories != nil {
			if _, exists := existingCategories[category.ID]; exists {
				continue
			}
		}
		if !containsPath(category.Patch.Paths, "name") || category.Patch.Name == "" {
			errs = append(errs, requiredSeedField(
				"category",
				category.Key,
				"name",
				fmt.Sprintf("categories[%d].name", index),
			))
		}
	}
	for index, product := range doc.Products {
		if existingProducts != nil {
			if _, exists := existingProducts[product.ID]; exists {
				continue
			}
		}
		path := fmt.Sprintf("products[%d]", index)
		if !containsPath(product.Patch.Paths, "name") || product.Patch.Name == "" {
			errs = append(errs, requiredSeedField("product", product.Key, "name", path+".name"))
		}
		if !containsPath(product.Patch.Paths, "price") || product.Patch.Price == nil {
			errs = append(errs, requiredSeedField("product", product.Key, "price_minor", path+".price_minor"))
			errs = append(errs, requiredSeedField("product", product.Key, "currency", path+".currency"))
		}
		if !containsPath(product.Patch.Paths, "availability") || product.Patch.Availability == "" {
			errs = append(errs, requiredSeedField("product", product.Key, "availability", path+".availability"))
		}
	}
	return joinSeedErrors(errs)
}

func loadCategoryParents(ctx context.Context, tx pgx.Tx) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT id::text, COALESCE(parent_id::text, '') FROM categories`)
	if err != nil {
		return nil, fmt.Errorf("load category parents: %w", err)
	}
	defer rows.Close()
	parents := make(map[string]string)
	for rows.Next() {
		var id, parent string
		if err := rows.Scan(&id, &parent); err != nil {
			return nil, fmt.Errorf("scan category parent: %w", err)
		}
		parents[id] = parent
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate category parents: %w", err)
	}
	return parents, nil
}

func loadProductIDs(ctx context.Context, tx pgx.Tx) (map[string]struct{}, error) {
	rows, err := tx.Query(ctx, `SELECT id::text FROM products`)
	if err != nil {
		return nil, fmt.Errorf("load product ids: %w", err)
	}
	defer rows.Close()
	ids := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan product id: %w", err)
		}
		ids[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate product ids: %w", err)
	}
	return ids, nil
}

func updateImportedCategory(ctx context.Context, tx pgx.Tx, category SeedCategory) error {
	sets := make([]string, 0, len(category.Patch.Paths)+2)
	arguments := []any{category.ID}
	for _, path := range category.Patch.Paths {
		switch path {
		case "name":
			arguments = append(arguments, category.Patch.Name)
			sets = append(sets, fmt.Sprintf("name = $%d", len(arguments)))
		case "parent_id":
			arguments = append(arguments, nullableText(category.Patch.ParentID))
			sets = append(sets, fmt.Sprintf("parent_id = $%d::uuid", len(arguments)))
		case "sort_order":
			arguments = append(arguments, category.Patch.SortOrder)
			sets = append(sets, fmt.Sprintf("sort_order = $%d", len(arguments)))
		}
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "version = version + 1", "updated_at = CURRENT_TIMESTAMP")
	if _, err := tx.Exec(ctx, `
		UPDATE categories SET `+strings.Join(sets, ", ")+`
		WHERE id = $1::uuid
	`, arguments...); err != nil {
		return fmt.Errorf("update imported category: %w", err)
	}
	return nil
}

func updateImportedProduct(ctx context.Context, tx pgx.Tx, product SeedProduct) error {
	sets := make([]string, 0, len(product.Patch.Paths)+2)
	arguments := []any{product.ID}
	for _, path := range product.Patch.Paths {
		switch path {
		case "name":
			arguments = append(arguments, product.Patch.Name)
			sets = append(sets, fmt.Sprintf("name = $%d", len(arguments)))
		case "description":
			arguments = append(arguments, product.Patch.Description)
			sets = append(sets, fmt.Sprintf("description = $%d", len(arguments)))
		case "price":
			arguments = append(arguments, product.Patch.Price.MinorUnits, product.Patch.Price.Currency)
			sets = append(sets,
				fmt.Sprintf("price_minor_units = $%d", len(arguments)-1),
				fmt.Sprintf("currency_code = $%d", len(arguments)),
			)
		case "availability":
			arguments = append(arguments, product.Patch.Availability)
			sets = append(sets, fmt.Sprintf("availability = $%d", len(arguments)))
		case "unit_of_measure":
			arguments = append(arguments, product.Patch.UnitOfMeasure)
			sets = append(sets, fmt.Sprintf("unit_of_measure = $%d", len(arguments)))
		case "min_order_quantity":
			arguments = append(arguments, product.Patch.MinOrderQuantity)
			sets = append(sets, fmt.Sprintf("min_order_quantity = $%d", len(arguments)))
		case "min_order_amount":
			arguments = append(arguments, nullableMinorUnits(product.Patch.MinOrderAmount))
			sets = append(sets, fmt.Sprintf("min_order_amount_minor_units = $%d", len(arguments)))
		}
	}
	sets = append(sets, "version = version + 1", "updated_at = CURRENT_TIMESTAMP")
	if _, err := tx.Exec(ctx, `
		UPDATE products SET `+strings.Join(sets, ", ")+`
		WHERE id = $1::uuid
	`, arguments...); err != nil {
		return fmt.Errorf("update imported product: %w", err)
	}
	if containsPath(product.Patch.Paths, "category_ids") {
		if _, err := tx.Exec(ctx, `DELETE FROM product_categories WHERE product_id = $1::uuid`, product.ID); err != nil {
			return fmt.Errorf("clear imported product categories: %w", err)
		}
		if err := insertProductCategories(ctx, tx, product.ID, product.Patch.CategoryIDs); err != nil {
			return err
		}
	}
	return nil
}

package catalog

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/identifier"
	"github.com/SV1Stail/online_market/internal/money"
	"github.com/SV1Stail/online_market/internal/pagination"
)

type AdminListFilter struct {
	CategoryIDs    []string
	Query          string
	Availabilities []Availability
	Cursor         *pagination.Cursor
	Limit          int
}

type CategoryPatch struct {
	Name      string
	ParentID  string
	SortOrder uint32
	Paths     []string
}

type ProductPatch struct {
	Name         string
	Description  string
	Price        *money.Money
	Availability Availability
	CategoryIDs  []string
	Paths        []string
}

type AdminRepository interface {
	ListAdminProducts(context.Context, AdminListFilter) ([]Product, error)
	GetAdminProduct(context.Context, string) (*Product, error)
	CreateProduct(context.Context, Product) (*Product, error)
	UpdateProduct(context.Context, string, uint64, ProductPatch) (*Product, error)
	ArchiveProduct(context.Context, string, uint64) (*Product, error)
	ListCategories(context.Context) ([]Category, error)
	GetCategory(context.Context, string) (*Category, error)
	CreateCategory(context.Context, Category) (*Category, error)
	UpdateCategory(context.Context, string, uint64, CategoryPatch) (*Category, error)
	DeleteCategory(context.Context, string, uint64) error
}

type AdminIDGenerator interface {
	UUID() (string, error)
}

type AdminService struct {
	repository AdminRepository
	ids        AdminIDGenerator
}

func NewAdminService(repository AdminRepository, ids AdminIDGenerator) *AdminService {
	return &AdminService{repository: repository, ids: ids}
}

func (s *AdminService) ListProducts(
	ctx context.Context,
	filter AdminListFilter,
	pageSize uint32,
	pageToken string,
) (*ListResult, error) {
	if err := validateCategoryIDs(filter.CategoryIDs); err != nil {
		return nil, err
	}
	if len(filter.Query) > 255 {
		return nil, apperror.Validation("filter.query", "must not exceed 255 characters")
	}
	if len(filter.Availabilities) > 3 {
		return nil, apperror.Validation("filter.availabilities", "must not contain more than 3 values")
	}
	for _, availability := range filter.Availabilities {
		if !validAvailability(availability) {
			return nil, apperror.Validation("filter.availabilities", "contains an unsupported value")
		}
	}
	cursor, err := pagination.Decode(pageToken)
	if err != nil {
		return nil, apperror.Validation("page.page_token", "invalid page token")
	}
	limit := pagination.PageSize(pageSize)
	filter.Query = strings.TrimSpace(filter.Query)
	filter.Cursor = cursor
	filter.Limit = limit + 1
	products, err := s.repository.ListAdminProducts(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list admin products: %w", err)
	}
	result := &ListResult{Products: products}
	if len(products) > limit {
		last := products[limit-1]
		result.Products = products[:limit]
		result.NextPageToken, err = pagination.Encode(pagination.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return nil, fmt.Errorf("encode admin products page: %w", err)
		}
	}
	return result, nil
}

func (s *AdminService) GetProduct(ctx context.Context, productID string) (*Product, error) {
	if !identifier.IsUUID(strings.TrimSpace(productID)) {
		return nil, apperror.Validation("product_id", "must be a valid UUID")
	}
	result, err := s.repository.GetAdminProduct(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("get admin product: %w", err)
	}
	return result, nil
}

func (s *AdminService) CreateProduct(ctx context.Context, patch ProductPatch) (*Product, error) {
	patch = normalizeProductPatch(patch)
	if err := validateProductPatch(patch, []string{"name", "description", "price", "availability", "category_ids"}); err != nil {
		return nil, err
	}
	id, err := s.ids.UUID()
	if err != nil {
		return nil, fmt.Errorf("generate product id: %w", err)
	}
	result, err := s.repository.CreateProduct(ctx, Product{
		ID: id, Name: patch.Name, Description: patch.Description, Price: *patch.Price,
		Availability: patch.Availability, CategoryIDs: patch.CategoryIDs, Version: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}
	return result, nil
}

func (s *AdminService) UpdateProduct(ctx context.Context, productID string, expectedVersion uint64, patch ProductPatch) (*Product, error) {
	current, err := s.GetProduct(ctx, productID)
	if err != nil {
		return nil, err
	}
	if expectedVersion != 0 && expectedVersion != current.Version {
		return nil, apperror.ErrConflict
	}
	patch = normalizeProductPatch(patch)
	if err := validateProductPatch(patch, patch.Paths); err != nil {
		return nil, err
	}
	result, err := s.repository.UpdateProduct(ctx, productID, current.Version, patch)
	if err != nil {
		return nil, fmt.Errorf("update product: %w", err)
	}
	return result, nil
}

func (s *AdminService) DeleteProduct(ctx context.Context, productID string, expectedVersion uint64) (*Product, error) {
	current, err := s.GetProduct(ctx, productID)
	if err != nil {
		return nil, err
	}
	if expectedVersion != 0 && expectedVersion != current.Version {
		return nil, apperror.ErrConflict
	}
	if current.Availability == AvailabilityArchived {
		return current, nil
	}
	result, err := s.repository.ArchiveProduct(ctx, productID, current.Version)
	if err != nil {
		return nil, fmt.Errorf("archive product: %w", err)
	}
	return result, nil
}

func (s *AdminService) ListCategories(ctx context.Context) ([]Category, error) {
	result, err := s.repository.ListCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list admin categories: %w", err)
	}
	return result, nil
}

func (s *AdminService) GetCategory(ctx context.Context, categoryID string) (*Category, error) {
	if !identifier.IsUUID(strings.TrimSpace(categoryID)) {
		return nil, apperror.Validation("category_id", "must be a valid UUID")
	}
	result, err := s.repository.GetCategory(ctx, categoryID)
	if err != nil {
		return nil, fmt.Errorf("get category: %w", err)
	}
	return result, nil
}

func (s *AdminService) CreateCategory(ctx context.Context, patch CategoryPatch) (*Category, error) {
	patch = normalizeCategoryPatch(patch)
	if err := validateCategoryPatch("", patch, []string{"name", "parent_id", "sort_order"}); err != nil {
		return nil, err
	}
	id, err := s.ids.UUID()
	if err != nil {
		return nil, fmt.Errorf("generate category id: %w", err)
	}
	result, err := s.repository.CreateCategory(ctx, Category{
		ID: id, Name: patch.Name, ParentID: patch.ParentID, SortOrder: patch.SortOrder, Version: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("create category: %w", err)
	}
	return result, nil
}

func (s *AdminService) UpdateCategory(ctx context.Context, categoryID string, expectedVersion uint64, patch CategoryPatch) (*Category, error) {
	current, err := s.GetCategory(ctx, categoryID)
	if err != nil {
		return nil, err
	}
	if expectedVersion != 0 && expectedVersion != current.Version {
		return nil, apperror.ErrConflict
	}
	patch = normalizeCategoryPatch(patch)
	if err := validateCategoryPatch(categoryID, patch, patch.Paths); err != nil {
		return nil, err
	}
	result, err := s.repository.UpdateCategory(ctx, categoryID, current.Version, patch)
	if err != nil {
		return nil, fmt.Errorf("update category: %w", err)
	}
	return result, nil
}

func (s *AdminService) DeleteCategory(ctx context.Context, categoryID string, expectedVersion uint64) error {
	current, err := s.GetCategory(ctx, categoryID)
	if err != nil {
		return err
	}
	if expectedVersion != 0 && expectedVersion != current.Version {
		return apperror.ErrConflict
	}
	if err := s.repository.DeleteCategory(ctx, categoryID, current.Version); err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	return nil
}

func normalizeProductPatch(patch ProductPatch) ProductPatch {
	patch.Name = strings.TrimSpace(patch.Name)
	patch.Description = strings.TrimSpace(patch.Description)
	patch.CategoryIDs = uniqueStrings(patch.CategoryIDs)
	return patch
}

func validateProductPatch(patch ProductPatch, paths []string) error {
	allowed := map[string]struct{}{
		"name": {}, "description": {}, "price": {}, "availability": {}, "category_ids": {},
	}
	if err := validateCatalogPaths(paths, allowed); err != nil {
		return err
	}
	if containsPath(paths, "name") && (patch.Name == "" || len(patch.Name) > 255) {
		return apperror.Validation("patch.name", "must contain between 1 and 255 characters")
	}
	if containsPath(paths, "description") && len(patch.Description) > 10_000 {
		return apperror.Validation("patch.description", "must not exceed 10000 characters")
	}
	if containsPath(paths, "price") && (patch.Price == nil || patch.Price.Currency != "RUB") {
		return apperror.Validation("patch.price", "must contain a non-negative RUB amount")
	}
	if containsPath(paths, "availability") && !validAvailability(patch.Availability) {
		return apperror.Validation("patch.availability", "must be specified")
	}
	if containsPath(paths, "category_ids") {
		if err := validateCategoryIDs(patch.CategoryIDs); err != nil {
			return err
		}
	}
	return nil
}

func normalizeCategoryPatch(patch CategoryPatch) CategoryPatch {
	patch.Name = strings.TrimSpace(patch.Name)
	patch.ParentID = strings.TrimSpace(patch.ParentID)
	return patch
}

func validateCategoryPatch(categoryID string, patch CategoryPatch, paths []string) error {
	allowed := map[string]struct{}{"name": {}, "parent_id": {}, "sort_order": {}}
	if err := validateCatalogPaths(paths, allowed); err != nil {
		return err
	}
	if containsPath(paths, "name") && (patch.Name == "" || len(patch.Name) > 255) {
		return apperror.Validation("patch.name", "must contain between 1 and 255 characters")
	}
	if containsPath(paths, "parent_id") && patch.ParentID != "" {
		if !identifier.IsUUID(patch.ParentID) {
			return apperror.Validation("patch.parent_id", "must be a valid UUID")
		}
		if patch.ParentID == categoryID {
			return apperror.Validation("patch.parent_id", "must not reference the category itself")
		}
	}
	if containsPath(paths, "sort_order") && patch.SortOrder > math.MaxInt32 {
		return apperror.Validation("patch.sort_order", "must not exceed 2147483647")
	}
	return nil
}

func validateCatalogPaths(paths []string, allowed map[string]struct{}) error {
	if len(paths) == 0 {
		return apperror.Validation("update_mask", "must contain at least one path")
	}
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if _, exists := allowed[path]; !exists {
			return apperror.Validation("update_mask", "unsupported path: "+path)
		}
		if _, exists := seen[path]; exists {
			return apperror.Validation("update_mask", "must not contain duplicate paths")
		}
		seen[path] = struct{}{}
	}
	return nil
}

func validateCategoryIDs(values []string) error {
	if len(values) > 50 {
		return apperror.Validation("category_ids", "must not contain more than 50 values")
	}
	for _, value := range values {
		if !identifier.IsUUID(strings.TrimSpace(value)) {
			return apperror.Validation("category_ids", "contains an invalid UUID")
		}
	}
	return nil
}

func validAvailability(value Availability) bool {
	return value == AvailabilityAvailable || value == AvailabilityOutOfStock || value == AvailabilityArchived
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

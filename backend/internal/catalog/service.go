package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/identifier"
	"github.com/SV1Stail/online_market/internal/pagination"
)

type Repository interface {
	ListProducts(context.Context, ListFilter) ([]Product, error)
	GetProduct(context.Context, string) (*Product, error)
	ListCategories(context.Context) ([]Category, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) ListProducts(
	ctx context.Context,
	categoryIDs []string,
	query string,
	onlyAvailable bool,
	pageSize uint32,
	pageToken string,
) (*ListResult, error) {
	if len(categoryIDs) > 50 {
		return nil, apperror.Validation("category_ids", "must not contain more than 50 values")
	}
	for _, categoryID := range categoryIDs {
		if !identifier.IsUUID(categoryID) {
			return nil, apperror.Validation("category_ids", "contains an invalid UUID")
		}
	}
	if len(query) > 255 {
		return nil, apperror.Validation("query", "must not exceed 255 characters")
	}
	cursor, err := pagination.Decode(pageToken)
	if err != nil {
		return nil, apperror.Validation("page.page_token", "invalid page token")
	}
	limit := pagination.PageSize(pageSize)
	products, err := s.repository.ListProducts(ctx, ListFilter{
		CategoryIDs:   categoryIDs,
		Query:         strings.TrimSpace(query),
		OnlyAvailable: onlyAvailable,
		Cursor:        cursor,
		Limit:         limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}

	result := &ListResult{Products: products}
	if len(products) > limit {
		last := products[limit-1]
		result.Products = products[:limit]
		result.NextPageToken, err = pagination.Encode(pagination.Cursor{
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("encode products page: %w", err)
		}
	}
	return result, nil
}

func (s *Service) GetProduct(ctx context.Context, productID string) (*Product, error) {
	if !identifier.IsUUID(strings.TrimSpace(productID)) {
		return nil, apperror.Validation("product_id", "must be a valid UUID")
	}
	product, err := s.repository.GetProduct(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("get product: %w", err)
	}
	return product, nil
}

func (s *Service) ListCategories(ctx context.Context) ([]Category, error) {
	categories, err := s.repository.ListCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	return categories, nil
}

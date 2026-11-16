package grpcapi

import (
	"context"

	marketv1 "github.com/SV1Stail/online_market/gen/online_market/v1"
	"github.com/SV1Stail/online_market/internal/catalog"
)

type CatalogServer struct {
	marketv1.UnimplementedCatalogServiceServer
	service *catalog.Service
}

func NewCatalogServer(service *catalog.Service) *CatalogServer {
	return &CatalogServer{service: service}
}

func (s *CatalogServer) ListProducts(ctx context.Context, request *marketv1.ListProductsRequest) (*marketv1.ListProductsResponse, error) {
	pageSize := uint32(0)
	pageToken := ""
	if request.GetPage() != nil {
		pageSize = request.GetPage().GetPageSize()
		pageToken = request.GetPage().GetPageToken()
	}
	result, err := s.service.ListProducts(
		ctx,
		request.GetCategoryIds(),
		request.GetQuery(),
		request.GetOnlyAvailable(),
		pageSize,
		pageToken,
	)
	if err != nil {
		return nil, err
	}
	products := make([]*marketv1.Product, 0, len(result.Products))
	for _, product := range result.Products {
		products = append(products, productToProto(product))
	}
	return &marketv1.ListProductsResponse{
		Products: products,
		Page:     &marketv1.PageResponse{NextPageToken: result.NextPageToken},
	}, nil
}

func (s *CatalogServer) GetProduct(ctx context.Context, request *marketv1.GetProductRequest) (*marketv1.GetProductResponse, error) {
	product, err := s.service.GetProduct(ctx, request.GetProductId())
	if err != nil {
		return nil, err
	}
	return &marketv1.GetProductResponse{Product: productToProto(*product)}, nil
}

func (s *CatalogServer) ListCategories(ctx context.Context, _ *marketv1.ListCategoriesRequest) (*marketv1.ListCategoriesResponse, error) {
	categories, err := s.service.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*marketv1.Category, 0, len(categories))
	for _, category := range categories {
		result = append(result, categoryToProto(category))
	}
	return &marketv1.ListCategoriesResponse{Categories: result}, nil
}

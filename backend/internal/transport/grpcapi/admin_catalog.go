package grpcapi

import (
	"context"

	marketv1 "github.com/SV1Stail/online_market/gen/online_market/v1"
	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/catalog"
)

type AdminCatalogServer struct {
	marketv1.UnimplementedAdminCatalogServiceServer
	service *catalog.AdminService
}

func NewAdminCatalogServer(service *catalog.AdminService) *AdminCatalogServer {
	return &AdminCatalogServer{service: service}
}

func (s *AdminCatalogServer) ListProducts(ctx context.Context, request *marketv1.ListAdminProductsRequest) (*marketv1.ListAdminProductsResponse, error) {
	filter := catalog.AdminListFilter{}
	if request.GetFilter() != nil {
		filter.CategoryIDs = request.GetFilter().GetCategoryIds()
		filter.Query = request.GetFilter().GetQuery()
		for _, value := range request.GetFilter().GetAvailabilities() {
			availability, err := availabilityFromProto(value)
			if err != nil {
				return nil, apperror.Validation("filter.availabilities", "contains an unspecified value")
			}
			filter.Availabilities = append(filter.Availabilities, availability)
		}
	}
	page := pageValues(request.GetPage())
	result, err := s.service.ListProducts(ctx, filter, page.Size, page.Token)
	if err != nil {
		return nil, err
	}
	products := make([]*marketv1.Product, 0, len(result.Products))
	for _, product := range result.Products {
		products = append(products, productToProto(product))
	}
	return &marketv1.ListAdminProductsResponse{
		Products: products,
		Page:     &marketv1.PageResponse{NextPageToken: result.NextPageToken},
	}, nil
}

func (s *AdminCatalogServer) GetProduct(ctx context.Context, request *marketv1.GetAdminProductRequest) (*marketv1.GetAdminProductResponse, error) {
	product, err := s.service.GetProduct(ctx, request.GetProductId())
	if err != nil {
		return nil, err
	}
	return &marketv1.GetAdminProductResponse{Product: productToProto(*product)}, nil
}

func (s *AdminCatalogServer) CreateProduct(ctx context.Context, request *marketv1.CreateProductRequest) (*marketv1.CreateProductResponse, error) {
	price, err := moneyFromProto(request.GetPrice())
	if err != nil {
		return nil, apperror.Validation("price", err.Error())
	}
	availability, err := availabilityFromProto(request.GetAvailability())
	if err != nil {
		return nil, apperror.Validation("availability", "must be specified")
	}
	minOrderAmount, err := moneyFromProto(request.GetMinOrderAmount())
	if err != nil {
		return nil, apperror.Validation("min_order_amount", err.Error())
	}
	product, err := s.service.CreateProduct(ctx, catalog.ProductPatch{
		Name: request.GetName(), Description: request.GetDescription(), Price: price,
		Availability: availability, CategoryIDs: request.GetCategoryIds(),
		UnitOfMeasure: request.GetUnitOfMeasure(), MinOrderQuantity: request.GetMinOrderQuantity(),
		MinOrderAmount: minOrderAmount,
	})
	if err != nil {
		return nil, err
	}
	return &marketv1.CreateProductResponse{Product: productToProto(*product)}, nil
}

func (s *AdminCatalogServer) UpdateProduct(ctx context.Context, request *marketv1.UpdateProductRequest) (*marketv1.UpdateProductResponse, error) {
	if request.GetPatch() == nil {
		return nil, apperror.Validation("patch", "is required")
	}
	price, err := moneyFromProto(request.GetPatch().GetPrice())
	if err != nil {
		return nil, apperror.Validation("patch.price", err.Error())
	}
	minOrderAmount, err := moneyFromProto(request.GetPatch().GetMinOrderAmount())
	if err != nil {
		return nil, apperror.Validation("patch.min_order_amount", err.Error())
	}
	availability := catalog.Availability("")
	if request.GetPatch().GetAvailability() != marketv1.ProductAvailability_PRODUCT_AVAILABILITY_UNSPECIFIED {
		availability, err = availabilityFromProto(request.GetPatch().GetAvailability())
		if err != nil {
			return nil, apperror.Validation("patch.availability", "contains an unsupported value")
		}
	}
	product, err := s.service.UpdateProduct(ctx, request.GetProductId(), request.GetExpectedVersion(), catalog.ProductPatch{
		Name: request.GetPatch().GetName(), Description: request.GetPatch().GetDescription(), Price: price,
		Availability: availability, CategoryIDs: request.GetPatch().GetCategoryIds(),
		UnitOfMeasure: request.GetPatch().GetUnitOfMeasure(), MinOrderQuantity: request.GetPatch().GetMinOrderQuantity(),
		MinOrderAmount: minOrderAmount, Paths: maskPaths(request.GetUpdateMask()),
	})
	if err != nil {
		return nil, err
	}
	return &marketv1.UpdateProductResponse{Product: productToProto(*product)}, nil
}

func (s *AdminCatalogServer) DeleteProduct(ctx context.Context, request *marketv1.DeleteProductRequest) (*marketv1.DeleteProductResponse, error) {
	product, err := s.service.DeleteProduct(ctx, request.GetProductId(), request.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	return &marketv1.DeleteProductResponse{Product: productToProto(*product)}, nil
}

func (s *AdminCatalogServer) ListCategories(ctx context.Context, _ *marketv1.ListAdminCategoriesRequest) (*marketv1.ListAdminCategoriesResponse, error) {
	categories, err := s.service.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*marketv1.Category, 0, len(categories))
	for _, category := range categories {
		result = append(result, categoryToProto(category))
	}
	return &marketv1.ListAdminCategoriesResponse{Categories: result}, nil
}

func (s *AdminCatalogServer) GetCategory(ctx context.Context, request *marketv1.GetAdminCategoryRequest) (*marketv1.GetAdminCategoryResponse, error) {
	category, err := s.service.GetCategory(ctx, request.GetCategoryId())
	if err != nil {
		return nil, err
	}
	return &marketv1.GetAdminCategoryResponse{Category: categoryToProto(*category)}, nil
}

func (s *AdminCatalogServer) CreateCategory(ctx context.Context, request *marketv1.CreateCategoryRequest) (*marketv1.CreateCategoryResponse, error) {
	category, err := s.service.CreateCategory(ctx, catalog.CategoryPatch{
		Name: request.GetName(), ParentID: request.GetParentId(), SortOrder: request.GetSortOrder(),
	})
	if err != nil {
		return nil, err
	}
	return &marketv1.CreateCategoryResponse{Category: categoryToProto(*category)}, nil
}

func (s *AdminCatalogServer) UpdateCategory(ctx context.Context, request *marketv1.UpdateCategoryRequest) (*marketv1.UpdateCategoryResponse, error) {
	if request.GetPatch() == nil {
		return nil, apperror.Validation("patch", "is required")
	}
	category, err := s.service.UpdateCategory(ctx, request.GetCategoryId(), request.GetExpectedVersion(), catalog.CategoryPatch{
		Name: request.GetPatch().GetName(), ParentID: request.GetPatch().GetParentId(),
		SortOrder: request.GetPatch().GetSortOrder(), Paths: maskPaths(request.GetUpdateMask()),
	})
	if err != nil {
		return nil, err
	}
	return &marketv1.UpdateCategoryResponse{Category: categoryToProto(*category)}, nil
}

func (s *AdminCatalogServer) DeleteCategory(ctx context.Context, request *marketv1.DeleteCategoryRequest) (*marketv1.DeleteCategoryResponse, error) {
	if err := s.service.DeleteCategory(ctx, request.GetCategoryId(), request.GetExpectedVersion()); err != nil {
		return nil, err
	}
	return &marketv1.DeleteCategoryResponse{}, nil
}

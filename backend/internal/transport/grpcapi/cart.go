package grpcapi

import (
	"context"

	marketv1 "github.com/SV1Stail/online_market/gen/online_market/v1"
	"github.com/SV1Stail/online_market/internal/cart"
)

type CartServer struct {
	marketv1.UnimplementedCartServiceServer
	service *cart.Service
}

func NewCartServer(service *cart.Service) *CartServer {
	return &CartServer{service: service}
}

func (s *CartServer) CreateCart(ctx context.Context, request *marketv1.CreateCartRequest) (*marketv1.CreateCartResponse, error) {
	result, err := s.service.Create(ctx, request.GetCurrencyCode())
	if err != nil {
		return nil, err
	}
	return &marketv1.CreateCartResponse{Cart: cartToProto(result.Cart), CartToken: result.Token}, nil
}

func (s *CartServer) GetCart(ctx context.Context, request *marketv1.GetCartRequest) (*marketv1.GetCartResponse, error) {
	result, err := s.service.Get(ctx, request.GetCartId(), request.GetCartToken())
	if err != nil {
		return nil, err
	}
	return &marketv1.GetCartResponse{Cart: cartToProto(result)}, nil
}

func (s *CartServer) AddCartItem(ctx context.Context, request *marketv1.AddCartItemRequest) (*marketv1.AddCartItemResponse, error) {
	result, err := s.service.AddItem(
		ctx, request.GetCartId(), request.GetCartToken(), request.GetProductId(), request.GetQuantity(), request.GetExpectedVersion(),
	)
	if err != nil {
		return nil, err
	}
	return &marketv1.AddCartItemResponse{Cart: cartToProto(result)}, nil
}

func (s *CartServer) UpdateCartItem(ctx context.Context, request *marketv1.UpdateCartItemRequest) (*marketv1.UpdateCartItemResponse, error) {
	result, err := s.service.UpdateItem(
		ctx, request.GetCartId(), request.GetCartToken(), request.GetItemId(), request.GetQuantity(), request.GetExpectedVersion(),
	)
	if err != nil {
		return nil, err
	}
	return &marketv1.UpdateCartItemResponse{Cart: cartToProto(result)}, nil
}

func (s *CartServer) RemoveCartItem(ctx context.Context, request *marketv1.RemoveCartItemRequest) (*marketv1.RemoveCartItemResponse, error) {
	result, err := s.service.RemoveItem(
		ctx, request.GetCartId(), request.GetCartToken(), request.GetItemId(), request.GetExpectedVersion(),
	)
	if err != nil {
		return nil, err
	}
	return &marketv1.RemoveCartItemResponse{Cart: cartToProto(result)}, nil
}

func (s *CartServer) ClearCart(ctx context.Context, request *marketv1.ClearCartRequest) (*marketv1.ClearCartResponse, error) {
	result, err := s.service.Clear(ctx, request.GetCartId(), request.GetCartToken(), request.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	return &marketv1.ClearCartResponse{Cart: cartToProto(result)}, nil
}

package grpcapi

import (
	"context"

	marketv1 "github.com/SV1Stail/online_market/gen/online_market/v1"
	"github.com/SV1Stail/online_market/internal/order"
)

type OrderServer struct {
	marketv1.UnimplementedOrderServiceServer
	service *order.Service
}

func NewOrderServer(service *order.Service) *OrderServer {
	return &OrderServer{service: service}
}

func (s *OrderServer) CreateOrder(ctx context.Context, request *marketv1.CreateOrderRequest) (*marketv1.CreateOrderResponse, error) {
	result, err := s.service.Create(ctx, order.CreateInput{
		CartID:          request.GetCartId(),
		CartToken:       request.GetCartToken(),
		Customer:        customerFromProto(request.GetCustomer()),
		Delivery:        deliveryFromProto(request.GetDelivery()),
		CustomerComment: request.GetCustomerComment(),
		IdempotencyKey:  request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, err
	}
	return &marketv1.CreateOrderResponse{Order: orderToProto(result.Order), OrderToken: result.Token}, nil
}

func (s *OrderServer) GetOrder(ctx context.Context, request *marketv1.GetOrderRequest) (*marketv1.GetOrderResponse, error) {
	result, err := s.service.Get(ctx, request.GetOrderNumber(), request.GetOrderToken())
	if err != nil {
		return nil, err
	}
	return &marketv1.GetOrderResponse{Order: orderToProto(result)}, nil
}

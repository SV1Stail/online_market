package grpcapi

import (
	"context"

	marketv1 "github.com/SV1Stail/online_market/gen/online_market/v1"
	"github.com/SV1Stail/online_market/internal/apperror"
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
	delivery, err := deliveryFromProto(request.GetDelivery())
	if err != nil {
		return nil, apperror.Validation("delivery.date", "must use YYYY-MM-DD")
	}
	result, err := s.service.Create(ctx, order.CreateInput{
		CartID:          request.GetCartId(),
		CartToken:       request.GetCartToken(),
		Customer:        customerFromProto(request.GetCustomer()),
		Delivery:        delivery,
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

func (s *OrderServer) ListDeliveryPlaces(ctx context.Context, _ *marketv1.ListDeliveryPlacesRequest) (*marketv1.ListDeliveryPlacesResponse, error) {
	places, err := s.service.ListDeliveryPlaces(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*marketv1.DeliveryPlace, 0, len(places))
	for _, place := range places {
		result = append(result, deliveryPlaceToProto(place))
	}
	return &marketv1.ListDeliveryPlacesResponse{Places: result}, nil
}

func (s *OrderServer) ListDeliveryDays(ctx context.Context, request *marketv1.ListDeliveryDaysRequest) (*marketv1.ListDeliveryDaysResponse, error) {
	from, err := order.ParseDeliveryDate(request.GetFromDate())
	if err != nil {
		return nil, apperror.Validation("from_date", "must use YYYY-MM-DD")
	}
	days, err := s.service.ListDeliveryDays(ctx, request.GetCity(), from, int(request.GetDays()))
	if err != nil {
		return nil, err
	}
	result := make([]*marketv1.DeliveryDay, 0, len(days))
	for _, day := range days {
		result = append(result, deliveryDayToProto(day))
	}
	return &marketv1.ListDeliveryDaysResponse{Days: result}, nil
}

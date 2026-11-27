package grpcapi

import (
	"context"

	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	marketv1 "github.com/SV1Stail/online_market/gen/online_market/v1"
	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/order"
)

type AdminOrderServer struct {
	marketv1.UnimplementedAdminOrderServiceServer
	service *order.AdminService
}

func NewAdminOrderServer(service *order.AdminService) *AdminOrderServer {
	return &AdminOrderServer{service: service}
}

func (s *AdminOrderServer) ListOrders(ctx context.Context, request *marketv1.ListAdminOrdersRequest) (*marketv1.ListAdminOrdersResponse, error) {
	filter := order.ListFilter{}
	if request.GetFilter() != nil {
		filter.Query = request.GetFilter().GetQuery()
		filter.AssignedAdminID = request.GetFilter().GetAssignedAdminId()
		if request.GetFilter().GetCreatedFrom() != nil {
			if err := request.GetFilter().GetCreatedFrom().CheckValid(); err != nil {
				return nil, apperror.Validation("filter.created_from", "invalid timestamp")
			}
			value := request.GetFilter().GetCreatedFrom().AsTime()
			filter.CreatedFrom = &value
		}
		if request.GetFilter().GetCreatedTo() != nil {
			if err := request.GetFilter().GetCreatedTo().CheckValid(); err != nil {
				return nil, apperror.Validation("filter.created_to", "invalid timestamp")
			}
			value := request.GetFilter().GetCreatedTo().AsTime()
			filter.CreatedTo = &value
		}
		for _, protoStatus := range request.GetFilter().GetStatuses() {
			statusValue, err := orderStatusFromProto(protoStatus)
			if err != nil {
				return nil, apperror.Validation("filter.statuses", "contains unspecified status")
			}
			filter.Statuses = append(filter.Statuses, statusValue)
		}
	}
	page := pageValues(request.GetPage())
	result, err := s.service.List(ctx, filter, page.Size, page.Token)
	if err != nil {
		return nil, err
	}
	orders := make([]*marketv1.AdminOrder, 0, len(result.Orders))
	for index := range result.Orders {
		orders = append(orders, adminOrderToProto(&result.Orders[index]))
	}
	return &marketv1.ListAdminOrdersResponse{
		Orders: orders,
		Page:   &marketv1.PageResponse{NextPageToken: result.NextPageToken},
	}, nil
}

func (s *AdminOrderServer) GetOrder(ctx context.Context, request *marketv1.GetAdminOrderRequest) (*marketv1.GetAdminOrderResponse, error) {
	result, err := s.service.Get(ctx, request.GetOrderId())
	if err != nil {
		return nil, err
	}
	return &marketv1.GetAdminOrderResponse{Order: adminOrderToProto(result)}, nil
}

func (s *AdminOrderServer) UpdateOrderDetails(ctx context.Context, request *marketv1.UpdateOrderDetailsRequest) (*marketv1.UpdateOrderDetailsResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	patch := request.GetPatch()
	if patch == nil {
		return nil, apperror.Validation("patch", "is required")
	}
	delivery, err := deliveryFromProto(patch.GetDelivery())
	if err != nil {
		return nil, apperror.Validation("delivery.date", "must use YYYY-MM-DD")
	}
	result, serviceErr := s.service.UpdateDetails(ctx, request.GetOrderId(), request.GetExpectedVersion(), principal.UserID, order.DetailsPatch{
		Name:            patch.GetName(),
		Customer:        customerFromProto(patch.GetCustomer()),
		Delivery:        delivery,
		CustomerComment: patch.GetCustomerComment(),
		InternalComment: patch.GetInternalComment(),
		AssignedAdminID: patch.GetAssignedAdminId(),
		Paths:           maskPaths(request.GetUpdateMask()),
	})
	if serviceErr != nil {
		return nil, serviceErr
	}
	return &marketv1.UpdateOrderDetailsResponse{Order: adminOrderToProto(result)}, nil
}

func (s *AdminOrderServer) AddOrderItem(ctx context.Context, request *marketv1.AddOrderItemRequest) (*marketv1.AddOrderItemResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	price, err := moneyFromProto(request.GetUnitPrice())
	if err != nil {
		return nil, apperror.Validation("unit_price", err.Error())
	}
	result, serviceErr := s.service.AddItem(
		ctx, request.GetOrderId(), request.GetExpectedVersion(), principal.UserID,
		request.GetProductId(), request.GetQuantity(), price,
	)
	if serviceErr != nil {
		return nil, serviceErr
	}
	return &marketv1.AddOrderItemResponse{Order: adminOrderToProto(result)}, nil
}

func (s *AdminOrderServer) UpdateOrderItem(ctx context.Context, request *marketv1.UpdateOrderItemRequest) (*marketv1.UpdateOrderItemResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	patch := request.GetPatch()
	if patch == nil {
		return nil, apperror.Validation("patch", "is required")
	}
	price, err := moneyFromProto(patch.GetUnitPrice())
	if err != nil {
		return nil, apperror.Validation("patch.unit_price", err.Error())
	}
	result, serviceErr := s.service.UpdateItem(
		ctx, request.GetOrderId(), request.GetItemId(), request.GetExpectedVersion(), principal.UserID,
		order.ItemPatch{
			ProductName: patch.GetProductName(), Quantity: patch.GetQuantity(),
			UnitPrice: price, Paths: maskPaths(request.GetUpdateMask()),
		},
	)
	if serviceErr != nil {
		return nil, serviceErr
	}
	return &marketv1.UpdateOrderItemResponse{Order: adminOrderToProto(result)}, nil
}

func (s *AdminOrderServer) RemoveOrderItem(ctx context.Context, request *marketv1.RemoveOrderItemRequest) (*marketv1.RemoveOrderItemResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	result, serviceErr := s.service.RemoveItem(
		ctx, request.GetOrderId(), request.GetItemId(), request.GetExpectedVersion(), principal.UserID,
	)
	if serviceErr != nil {
		return nil, serviceErr
	}
	return &marketv1.RemoveOrderItemResponse{Order: adminOrderToProto(result)}, nil
}

func (s *AdminOrderServer) ChangeOrderStatus(ctx context.Context, request *marketv1.ChangeOrderStatusRequest) (*marketv1.ChangeOrderStatusResponse, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	statusValue, err := orderStatusFromProto(request.GetStatus())
	if err != nil {
		return nil, apperror.Validation("status", "must be specified")
	}
	result, serviceErr := s.service.ChangeStatus(
		ctx, request.GetOrderId(), request.GetExpectedVersion(), principal.UserID, statusValue, request.GetComment(),
	)
	if serviceErr != nil {
		return nil, serviceErr
	}
	return &marketv1.ChangeOrderStatusResponse{Order: adminOrderToProto(result)}, nil
}

func (s *AdminOrderServer) ListOrderEvents(ctx context.Context, request *marketv1.ListOrderEventsRequest) (*marketv1.ListOrderEventsResponse, error) {
	page := pageValues(request.GetPage())
	result, err := s.service.ListEvents(ctx, request.GetOrderId(), page.Size, page.Token)
	if err != nil {
		return nil, err
	}
	events := make([]*marketv1.OrderEvent, 0, len(result.Events))
	for _, event := range result.Events {
		changes := make([]*marketv1.OrderFieldChange, 0, len(event.Changes))
		for _, current := range event.Changes {
			changes = append(changes, &marketv1.OrderFieldChange{
				FieldPath: current.FieldPath, OldValue: current.OldValue, NewValue: current.NewValue,
			})
		}
		events = append(events, &marketv1.OrderEvent{
			Id:           event.ID,
			OrderId:      event.OrderID,
			Type:         eventTypeToProto(event.Type),
			ActorAdminId: event.ActorAdminID,
			Comment:      event.Comment,
			Changes:      changes,
			CreatedAt:    timestamppb.New(event.CreatedAt),
		})
	}
	return &marketv1.ListOrderEventsResponse{
		Events: events,
		Page:   &marketv1.PageResponse{NextPageToken: result.NextPageToken},
	}, nil
}

func (s *AdminOrderServer) SetDeliveryDayLimit(ctx context.Context, request *marketv1.SetDeliveryDayLimitRequest) (*marketv1.SetDeliveryDayLimitResponse, error) {
	if _, err := principalFromContext(ctx); err != nil {
		return nil, err
	}
	date, err := order.ParseDeliveryDate(request.GetDate())
	if err != nil || date.IsZero() {
		return nil, apperror.Validation("date", "must use YYYY-MM-DD")
	}
	day, err := s.service.SetDeliveryDayLimit(ctx, date, int(request.GetDeliveryLimit()))
	if err != nil {
		return nil, err
	}
	return &marketv1.SetDeliveryDayLimitResponse{Day: deliveryDayToProto(day)}, nil
}

func (s *AdminOrderServer) CreateDeliveryPlace(ctx context.Context, request *marketv1.CreateDeliveryPlaceRequest) (*marketv1.CreateDeliveryPlaceResponse, error) {
	if _, err := principalFromContext(ctx); err != nil {
		return nil, err
	}
	place, err := deliveryPlaceFromInput(request.GetName(), request.GetDailyLimit(), request.GetScheduleKind(), request.GetDates())
	if err != nil {
		return nil, err
	}
	created, err := s.service.CreateDeliveryPlace(ctx, place)
	if err != nil {
		return nil, err
	}
	return &marketv1.CreateDeliveryPlaceResponse{Place: deliveryPlaceToProto(created)}, nil
}

func (s *AdminOrderServer) UpdateDeliveryPlace(ctx context.Context, request *marketv1.UpdateDeliveryPlaceRequest) (*marketv1.UpdateDeliveryPlaceResponse, error) {
	if _, err := principalFromContext(ctx); err != nil {
		return nil, err
	}
	place, err := deliveryPlaceFromInput(request.GetName(), request.GetDailyLimit(), request.GetScheduleKind(), request.GetDates())
	if err != nil {
		return nil, err
	}
	place.ID = request.GetPlaceId()
	updated, err := s.service.UpdateDeliveryPlace(ctx, place)
	if err != nil {
		return nil, err
	}
	return &marketv1.UpdateDeliveryPlaceResponse{Place: deliveryPlaceToProto(updated)}, nil
}

func (s *AdminOrderServer) DeleteDeliveryPlace(ctx context.Context, request *marketv1.DeleteDeliveryPlaceRequest) (*marketv1.DeleteDeliveryPlaceResponse, error) {
	if _, err := principalFromContext(ctx); err != nil {
		return nil, err
	}
	if err := s.service.DeleteDeliveryPlace(ctx, request.GetPlaceId()); err != nil {
		return nil, err
	}
	return &marketv1.DeleteDeliveryPlaceResponse{}, nil
}

type pageInput struct {
	Size  uint32
	Token string
}

func pageValues(page *marketv1.PageRequest) *pageInput {
	if page == nil {
		return &pageInput{}
	}
	return &pageInput{Size: page.GetPageSize(), Token: page.GetPageToken()}
}

func maskPaths(mask *fieldmaskpb.FieldMask) []string {
	if mask == nil {
		return nil
	}
	return append([]string(nil), mask.GetPaths()...)
}

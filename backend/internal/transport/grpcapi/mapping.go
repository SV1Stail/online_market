package grpcapi

import (
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"

	marketv1 "github.com/SV1Stail/online_market/gen/online_market/v1"
	"github.com/SV1Stail/online_market/internal/auth"
	"github.com/SV1Stail/online_market/internal/cart"
	"github.com/SV1Stail/online_market/internal/catalog"
	"github.com/SV1Stail/online_market/internal/money"
	"github.com/SV1Stail/online_market/internal/order"
)

func moneyToProto(value money.Money) *marketv1.Money {
	return &marketv1.Money{MinorUnits: value.MinorUnits, CurrencyCode: value.Currency}
}

func moneyFromProto(value *marketv1.Money) (*money.Money, error) {
	if value == nil {
		return nil, nil
	}
	result, err := money.New(value.GetMinorUnits(), value.GetCurrencyCode())
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func productToProto(value catalog.Product) *marketv1.Product {
	return &marketv1.Product{
		Id:           value.ID,
		Name:         value.Name,
		Description:  value.Description,
		Price:        moneyToProto(value.Price),
		Availability: availabilityToProto(value.Availability),
		CategoryIds:  append([]string(nil), value.CategoryIDs...),
		CreatedAt:    timestamppb.New(value.CreatedAt),
		UpdatedAt:    timestamppb.New(value.UpdatedAt),
		Version:      value.Version,
	}
}

func categoryToProto(value catalog.Category) *marketv1.Category {
	return &marketv1.Category{
		Id: value.ID, Name: value.Name, ParentId: value.ParentID, SortOrder: value.SortOrder,
		Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt),
	}
}

func availabilityToProto(value catalog.Availability) marketv1.ProductAvailability {
	switch value {
	case catalog.AvailabilityAvailable:
		return marketv1.ProductAvailability_PRODUCT_AVAILABILITY_AVAILABLE
	case catalog.AvailabilityOutOfStock:
		return marketv1.ProductAvailability_PRODUCT_AVAILABILITY_OUT_OF_STOCK
	case catalog.AvailabilityArchived:
		return marketv1.ProductAvailability_PRODUCT_AVAILABILITY_ARCHIVED
	default:
		return marketv1.ProductAvailability_PRODUCT_AVAILABILITY_UNSPECIFIED
	}
}

func availabilityFromProto(value marketv1.ProductAvailability) (catalog.Availability, error) {
	switch value {
	case marketv1.ProductAvailability_PRODUCT_AVAILABILITY_AVAILABLE:
		return catalog.AvailabilityAvailable, nil
	case marketv1.ProductAvailability_PRODUCT_AVAILABILITY_OUT_OF_STOCK:
		return catalog.AvailabilityOutOfStock, nil
	case marketv1.ProductAvailability_PRODUCT_AVAILABILITY_ARCHIVED:
		return catalog.AvailabilityArchived, nil
	default:
		return "", fmt.Errorf("unspecified product availability")
	}
}

func cartToProto(value *cart.Cart) *marketv1.Cart {
	items := make([]*marketv1.CartItem, 0, len(value.Items))
	for _, item := range value.Items {
		items = append(items, &marketv1.CartItem{
			Id:           item.ID,
			ProductId:    item.ProductID,
			ProductName:  item.ProductName,
			UnitPrice:    moneyToProto(item.UnitPrice),
			Quantity:     item.Quantity,
			LineTotal:    moneyToProto(item.LineTotal),
			Availability: availabilityToProto(item.Availability),
		})
	}
	return &marketv1.Cart{
		Id:        value.ID,
		Status:    cartStatusToProto(value.Status),
		Items:     items,
		Total:     moneyToProto(value.Total),
		Version:   value.Version,
		CreatedAt: timestamppb.New(value.CreatedAt),
		UpdatedAt: timestamppb.New(value.UpdatedAt),
		ExpiresAt: timestamppb.New(value.ExpiresAt),
	}
}

func cartStatusToProto(value cart.Status) marketv1.CartStatus {
	switch value {
	case cart.StatusOpen:
		return marketv1.CartStatus_CART_STATUS_OPEN
	case cart.StatusCheckedOut:
		return marketv1.CartStatus_CART_STATUS_CHECKED_OUT
	case cart.StatusExpired:
		return marketv1.CartStatus_CART_STATUS_EXPIRED
	default:
		return marketv1.CartStatus_CART_STATUS_UNSPECIFIED
	}
}

func orderToProto(value *order.Order) *marketv1.Order {
	items := make([]*marketv1.OrderItem, 0, len(value.Items))
	for _, item := range value.Items {
		items = append(items, &marketv1.OrderItem{
			Id:          item.ID,
			ProductId:   item.ProductID,
			ProductName: item.ProductName,
			UnitPrice:   moneyToProto(item.UnitPrice),
			Quantity:    item.Quantity,
			LineTotal:   moneyToProto(item.LineTotal),
		})
	}
	return &marketv1.Order{
		Id:              value.ID,
		Number:          value.Number,
		Name:            value.Name,
		Customer:        customerToProto(value.Customer),
		Delivery:        deliveryToProto(value.Delivery),
		CustomerComment: value.CustomerComment,
		Status:          orderStatusToProto(value.Status),
		Items:           items,
		Total:           moneyToProto(value.Total),
		Version:         value.Version,
		CreatedAt:       timestamppb.New(value.CreatedAt),
		UpdatedAt:       timestamppb.New(value.UpdatedAt),
	}
}

func adminOrderToProto(value *order.Order) *marketv1.AdminOrder {
	return &marketv1.AdminOrder{
		Order:           orderToProto(value),
		InternalComment: value.InternalComment,
		AssignedAdminId: value.AssignedAdminID,
	}
}

func customerToProto(value order.Customer) *marketv1.CustomerContact {
	return &marketv1.CustomerContact{FullName: value.FullName, Phone: value.Phone, Email: value.Email}
}

func customerFromProto(value *marketv1.CustomerContact) order.Customer {
	if value == nil {
		return order.Customer{}
	}
	return order.Customer{FullName: value.GetFullName(), Phone: value.GetPhone(), Email: value.GetEmail()}
}

func deliveryToProto(value order.Delivery) *marketv1.DeliveryDetails {
	return &marketv1.DeliveryDetails{
		Method:  deliveryMethodToProto(value.Method),
		City:    value.City,
		Address: value.Address,
		Comment: value.Comment,
	}
}

func deliveryFromProto(value *marketv1.DeliveryDetails) order.Delivery {
	if value == nil {
		return order.Delivery{}
	}
	return order.Delivery{
		Method: deliveryMethodFromProto(value.GetMethod()),
		City:   value.GetCity(), Address: value.GetAddress(), Comment: value.GetComment(),
	}
}

func deliveryMethodToProto(value order.DeliveryMethod) marketv1.DeliveryMethod {
	switch value {
	case order.DeliveryPickup:
		return marketv1.DeliveryMethod_DELIVERY_METHOD_PICKUP
	case order.DeliveryCourier:
		return marketv1.DeliveryMethod_DELIVERY_METHOD_COURIER
	default:
		return marketv1.DeliveryMethod_DELIVERY_METHOD_UNSPECIFIED
	}
}

func deliveryMethodFromProto(value marketv1.DeliveryMethod) order.DeliveryMethod {
	switch value {
	case marketv1.DeliveryMethod_DELIVERY_METHOD_PICKUP:
		return order.DeliveryPickup
	case marketv1.DeliveryMethod_DELIVERY_METHOD_COURIER:
		return order.DeliveryCourier
	default:
		return ""
	}
}

func orderStatusToProto(value order.Status) marketv1.OrderStatus {
	switch value {
	case order.StatusNew:
		return marketv1.OrderStatus_ORDER_STATUS_NEW
	case order.StatusConfirmed:
		return marketv1.OrderStatus_ORDER_STATUS_CONFIRMED
	case order.StatusAssembling:
		return marketv1.OrderStatus_ORDER_STATUS_ASSEMBLING
	case order.StatusReady:
		return marketv1.OrderStatus_ORDER_STATUS_READY
	case order.StatusInDelivery:
		return marketv1.OrderStatus_ORDER_STATUS_IN_DELIVERY
	case order.StatusCompleted:
		return marketv1.OrderStatus_ORDER_STATUS_COMPLETED
	case order.StatusCancelled:
		return marketv1.OrderStatus_ORDER_STATUS_CANCELLED
	default:
		return marketv1.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

func orderStatusFromProto(value marketv1.OrderStatus) (order.Status, error) {
	var statusValue order.Status
	switch value {
	case marketv1.OrderStatus_ORDER_STATUS_NEW:
		statusValue = order.StatusNew
	case marketv1.OrderStatus_ORDER_STATUS_CONFIRMED:
		statusValue = order.StatusConfirmed
	case marketv1.OrderStatus_ORDER_STATUS_ASSEMBLING:
		statusValue = order.StatusAssembling
	case marketv1.OrderStatus_ORDER_STATUS_READY:
		statusValue = order.StatusReady
	case marketv1.OrderStatus_ORDER_STATUS_IN_DELIVERY:
		statusValue = order.StatusInDelivery
	case marketv1.OrderStatus_ORDER_STATUS_COMPLETED:
		statusValue = order.StatusCompleted
	case marketv1.OrderStatus_ORDER_STATUS_CANCELLED:
		statusValue = order.StatusCancelled
	default:
		return "", fmt.Errorf("unspecified order status")
	}
	return statusValue, nil
}

func authUserToProto(value auth.User) *marketv1.AdminUser {
	roles := make([]marketv1.AdminRole, 0, len(value.Roles))
	for _, role := range value.Roles {
		switch role {
		case auth.RoleManager:
			roles = append(roles, marketv1.AdminRole_ADMIN_ROLE_MANAGER)
		case auth.RoleAdmin:
			roles = append(roles, marketv1.AdminRole_ADMIN_ROLE_ADMIN)
		}
	}
	return &marketv1.AdminUser{Id: value.ID, DisplayName: value.DisplayName, Roles: roles}
}

func eventTypeToProto(value order.EventType) marketv1.OrderEventType {
	switch value {
	case order.EventCreated:
		return marketv1.OrderEventType_ORDER_EVENT_TYPE_CREATED
	case order.EventDetailsChanged:
		return marketv1.OrderEventType_ORDER_EVENT_TYPE_DETAILS_CHANGED
	case order.EventItemAdded:
		return marketv1.OrderEventType_ORDER_EVENT_TYPE_ITEM_ADDED
	case order.EventItemChanged:
		return marketv1.OrderEventType_ORDER_EVENT_TYPE_ITEM_CHANGED
	case order.EventItemRemoved:
		return marketv1.OrderEventType_ORDER_EVENT_TYPE_ITEM_REMOVED
	case order.EventStatusChanged:
		return marketv1.OrderEventType_ORDER_EVENT_TYPE_STATUS_CHANGED
	default:
		return marketv1.OrderEventType_ORDER_EVENT_TYPE_UNSPECIFIED
	}
}

package order

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/identifier"
	"github.com/SV1Stail/online_market/internal/money"
	"github.com/SV1Stail/online_market/internal/pagination"
)

type ListFilter struct {
	Statuses        []Status
	Query           string
	CreatedFrom     *time.Time
	CreatedTo       *time.Time
	AssignedAdminID string
	Cursor          *pagination.Cursor
	Limit           int
}

type ListResult struct {
	Orders        []Order
	NextPageToken string
}

type EventListResult struct {
	Events        []Event
	NextPageToken string
}

type DetailsPatch struct {
	Name            string
	Customer        Customer
	Delivery        Delivery
	CustomerComment string
	InternalComment string
	AssignedAdminID string
	Paths           []string
}

type ItemPatch struct {
	ProductName string
	Quantity    uint32
	UnitPrice   *money.Money
	Paths       []string
}

type AdminRepository interface {
	List(context.Context, ListFilter) ([]Order, error)
	GetByID(context.Context, string) (*Order, error)
	UpdateDetails(context.Context, string, uint64, string, DetailsPatch, []string) (*Order, error)
	AddItem(context.Context, string, uint64, string, string, uint32, *money.Money, []string) (*Order, error)
	UpdateItem(context.Context, string, string, uint64, string, ItemPatch, []string) (*Order, error)
	RemoveItem(context.Context, string, string, uint64, string, []string) (*Order, error)
	ChangeStatus(context.Context, string, uint64, string, Status, Status, string, []string) (*Order, error)
	ListEvents(context.Context, string, *pagination.Cursor, int) ([]Event, error)
}

type AdminService struct {
	repository           AdminRepository
	notificationChannels []string
}

func NewAdminService(repository AdminRepository, notificationChannels []string) *AdminService {
	return &AdminService{
		repository:           repository,
		notificationChannels: append([]string(nil), notificationChannels...),
	}
}

func (s *AdminService) List(ctx context.Context, filter ListFilter, pageSize uint32, pageToken string) (*ListResult, error) {
	if len(filter.Query) > 255 {
		return nil, apperror.Validation("filter.query", "must not exceed 255 characters")
	}
	if len(filter.Statuses) > 7 {
		return nil, apperror.Validation("filter.statuses", "must not contain more than 7 values")
	}
	if filter.AssignedAdminID != "" && !identifier.IsUUID(filter.AssignedAdminID) {
		return nil, apperror.Validation("filter.assigned_admin_id", "must be a valid UUID")
	}
	if filter.CreatedFrom != nil && filter.CreatedTo != nil && !filter.CreatedFrom.Before(*filter.CreatedTo) {
		return nil, apperror.Validation("filter.created_to", "must be later than created_from")
	}
	cursor, err := pagination.Decode(pageToken)
	if err != nil {
		return nil, apperror.Validation("page.page_token", "invalid page token")
	}
	filter.Cursor = cursor
	filter.Limit = pagination.PageSize(pageSize) + 1
	filter.Query = strings.TrimSpace(filter.Query)
	orders, err := s.repository.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list admin orders: %w", err)
	}
	limit := filter.Limit - 1
	result := &ListResult{Orders: orders}
	if len(orders) > limit {
		last := orders[limit-1]
		result.Orders = orders[:limit]
		result.NextPageToken, err = pagination.Encode(pagination.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return nil, fmt.Errorf("encode orders page: %w", err)
		}
	}
	return result, nil
}

func (s *AdminService) Get(ctx context.Context, orderID string) (*Order, error) {
	if !identifier.IsUUID(strings.TrimSpace(orderID)) {
		return nil, apperror.Validation("order_id", "must be a valid UUID")
	}
	result, err := s.repository.GetByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("get admin order: %w", err)
	}
	return result, nil
}

func (s *AdminService) UpdateDetails(ctx context.Context, orderID string, expectedVersion uint64, actorID string, patch DetailsPatch) (*Order, error) {
	patch.Name = strings.TrimSpace(patch.Name)
	patch.Customer.FullName = strings.TrimSpace(patch.Customer.FullName)
	patch.Customer.Phone = strings.TrimSpace(patch.Customer.Phone)
	patch.Customer.Email = strings.TrimSpace(patch.Customer.Email)
	patch.Delivery.City = strings.TrimSpace(patch.Delivery.City)
	patch.Delivery.Address = strings.TrimSpace(patch.Delivery.Address)
	patch.Delivery.Comment = strings.TrimSpace(patch.Delivery.Comment)
	patch.CustomerComment = strings.TrimSpace(patch.CustomerComment)
	patch.InternalComment = strings.TrimSpace(patch.InternalComment)
	patch.AssignedAdminID = strings.TrimSpace(patch.AssignedAdminID)
	current, err := s.editableOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	allowed := map[string]struct{}{
		"name": {}, "customer": {}, "delivery": {}, "customer_comment": {},
		"internal_comment": {}, "assigned_admin_id": {},
	}
	if err := validatePaths(patch.Paths, allowed); err != nil {
		return nil, err
	}
	if contains(patch.Paths, "name") && (strings.TrimSpace(patch.Name) == "" || len(patch.Name) > 255) {
		return nil, apperror.Validation("patch.name", "must contain between 1 and 255 characters")
	}
	if contains(patch.Paths, "customer") {
		if err := validateCustomer(patch.Customer); err != nil {
			return nil, err
		}
	}
	if contains(patch.Paths, "delivery") {
		if err := validateDelivery(patch.Delivery); err != nil {
			return nil, err
		}
	}
	if contains(patch.Paths, "assigned_admin_id") && patch.AssignedAdminID != "" && !identifier.IsUUID(patch.AssignedAdminID) {
		return nil, apperror.Validation("patch.assigned_admin_id", "must be a valid UUID")
	}
	if contains(patch.Paths, "customer_comment") && len(patch.CustomerComment) > 4000 {
		return nil, apperror.Validation("patch.customer_comment", "must not exceed 4000 characters")
	}
	if contains(patch.Paths, "internal_comment") && len(patch.InternalComment) > 4000 {
		return nil, apperror.Validation("patch.internal_comment", "must not exceed 4000 characters")
	}
	result, err := s.repository.UpdateDetails(
		ctx, orderID, effectiveVersion(expectedVersion, current.Version), actorID, patch, s.notificationChannels,
	)
	if err != nil {
		return nil, fmt.Errorf("update order details: %w", err)
	}
	return result, nil
}

func (s *AdminService) AddItem(ctx context.Context, orderID string, expectedVersion uint64, actorID, productID string, quantity uint32, price *money.Money) (*Order, error) {
	current, err := s.editableOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if !identifier.IsUUID(strings.TrimSpace(productID)) {
		return nil, apperror.Validation("product_id", "must be a valid UUID")
	}
	if quantity == 0 || quantity > 10_000 {
		return nil, apperror.Validation("quantity", "must be between 1 and 10000")
	}
	if price != nil && price.Currency != current.Total.Currency {
		return nil, apperror.Validation("unit_price.currency_code", "must match order currency")
	}
	result, err := s.repository.AddItem(
		ctx, orderID, effectiveVersion(expectedVersion, current.Version), actorID,
		productID, quantity, price, s.notificationChannels,
	)
	if err != nil {
		return nil, fmt.Errorf("add order item: %w", err)
	}
	return result, nil
}

func (s *AdminService) UpdateItem(ctx context.Context, orderID, itemID string, expectedVersion uint64, actorID string, patch ItemPatch) (*Order, error) {
	patch.ProductName = strings.TrimSpace(patch.ProductName)
	if !identifier.IsUUID(strings.TrimSpace(itemID)) {
		return nil, apperror.Validation("item_id", "must be a valid UUID")
	}
	current, err := s.editableOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	allowed := map[string]struct{}{"product_name": {}, "quantity": {}, "unit_price": {}}
	if err := validatePaths(patch.Paths, allowed); err != nil {
		return nil, err
	}
	if contains(patch.Paths, "product_name") && (strings.TrimSpace(patch.ProductName) == "" || len(patch.ProductName) > 255) {
		return nil, apperror.Validation("patch.product_name", "must contain between 1 and 255 characters")
	}
	if contains(patch.Paths, "quantity") && (patch.Quantity == 0 || patch.Quantity > 10_000) {
		return nil, apperror.Validation("patch.quantity", "must be between 1 and 10000")
	}
	if contains(patch.Paths, "unit_price") {
		if patch.UnitPrice == nil || patch.UnitPrice.Currency != current.Total.Currency {
			return nil, apperror.Validation("patch.unit_price", "must use the order currency")
		}
	}
	result, err := s.repository.UpdateItem(
		ctx, orderID, itemID, effectiveVersion(expectedVersion, current.Version), actorID, patch, s.notificationChannels,
	)
	if err != nil {
		return nil, fmt.Errorf("update order item: %w", err)
	}
	return result, nil
}

func (s *AdminService) RemoveItem(ctx context.Context, orderID, itemID string, expectedVersion uint64, actorID string) (*Order, error) {
	if !identifier.IsUUID(strings.TrimSpace(itemID)) {
		return nil, apperror.Validation("item_id", "must be a valid UUID")
	}
	current, err := s.editableOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	result, err := s.repository.RemoveItem(
		ctx, orderID, itemID, effectiveVersion(expectedVersion, current.Version), actorID, s.notificationChannels,
	)
	if err != nil {
		return nil, fmt.Errorf("remove order item: %w", err)
	}
	return result, nil
}

func (s *AdminService) ChangeStatus(ctx context.Context, orderID string, expectedVersion uint64, actorID string, target Status, comment string) (*Order, error) {
	if len(comment) > 4000 {
		return nil, apperror.Validation("comment", "must not exceed 4000 characters")
	}
	current, err := s.Get(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if !CanTransition(current.Status, target) {
		return nil, fmt.Errorf("%w: transition from %s to %s is not allowed", apperror.ErrPrecondition, current.Status, target)
	}
	result, err := s.repository.ChangeStatus(
		ctx, orderID, effectiveVersion(expectedVersion, current.Version), actorID,
		current.Status, target, strings.TrimSpace(comment), s.notificationChannels,
	)
	if err != nil {
		return nil, fmt.Errorf("change order status: %w", err)
	}
	return result, nil
}

func (s *AdminService) ListEvents(ctx context.Context, orderID string, pageSize uint32, pageToken string) (*EventListResult, error) {
	if !identifier.IsUUID(strings.TrimSpace(orderID)) {
		return nil, apperror.Validation("order_id", "must be a valid UUID")
	}
	if _, err := s.Get(ctx, orderID); err != nil {
		return nil, err
	}
	cursor, err := pagination.Decode(pageToken)
	if err != nil {
		return nil, apperror.Validation("page.page_token", "invalid page token")
	}
	limit := pagination.PageSize(pageSize)
	events, err := s.repository.ListEvents(ctx, orderID, cursor, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list order events: %w", err)
	}
	result := &EventListResult{Events: events}
	if len(events) > limit {
		last := events[limit-1]
		result.Events = events[:limit]
		result.NextPageToken, err = pagination.Encode(pagination.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return nil, fmt.Errorf("encode events page: %w", err)
		}
	}
	return result, nil
}

func (s *AdminService) editableOrder(ctx context.Context, orderID string) (*Order, error) {
	current, err := s.Get(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if current.Status == StatusCompleted || current.Status == StatusCancelled {
		return nil, fmt.Errorf("%w: terminal order cannot be edited", apperror.ErrPrecondition)
	}
	return current, nil
}

func validatePaths(paths []string, allowed map[string]struct{}) error {
	if len(paths) == 0 {
		return apperror.Validation("update_mask", "must contain at least one path")
	}
	for _, path := range paths {
		if _, ok := allowed[path]; !ok {
			return apperror.Validation("update_mask", "unsupported path: "+path)
		}
	}
	return nil
}

func contains(paths []string, target string) bool {
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}

func effectiveVersion(requested, current uint64) uint64 {
	if requested != 0 {
		return requested
	}
	return current
}

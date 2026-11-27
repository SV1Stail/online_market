package e2e_test

import (
	"bytes"
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/auth"
	"github.com/SV1Stail/online_market/internal/cart"
	"github.com/SV1Stail/online_market/internal/catalog"
	"github.com/SV1Stail/online_market/internal/money"
	"github.com/SV1Stail/online_market/internal/order"
	"github.com/SV1Stail/online_market/internal/pagination"
)

type storedOrder struct {
	value       order.Order
	tokenHash   []byte
	requestHash []byte
}

type orderEventRecord = order.Event

type memoryOrderRepository struct {
	state *memoryState
	clock fixedClock
}

var _ order.Repository = (*memoryOrderRepository)(nil)

func (r *memoryOrderRepository) Create(_ context.Context, command order.CheckoutCommand) (*order.Order, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()

	if orderID, exists := r.state.idempotency[command.IdempotencyKey]; exists {
		stored := r.state.orders[orderID]
		cartValue, cartExists := r.state.carts[stored.value.CartID]
		if !cartExists || !bytes.Equal(cartValue.tokenHash, command.CartTokenHash) {
			return nil, apperror.ErrUnauthenticated
		}
		if !bytes.Equal(stored.requestHash, command.RequestHash) {
			return nil, apperror.ErrPrecondition
		}
		result := cloneOrder(stored.value)
		return &result, nil
	}

	cartValue, exists := r.state.carts[command.CartID]
	if !exists || !bytes.Equal(cartValue.tokenHash, command.CartTokenHash) {
		return nil, apperror.ErrUnauthenticated
	}
	if cartValue.cart.Status != cart.StatusOpen || !command.CreatedAt.Before(cartValue.cart.ExpiresAt) {
		return nil, apperror.ErrPrecondition
	}
	if len(cartValue.cart.Items) == 0 {
		return nil, apperror.ErrPrecondition
	}

	items := make([]order.Item, 0, len(cartValue.cart.Items))
	total, err := money.New(0, "RUB")
	if err != nil {
		return nil, err
	}
	for index, cartItem := range cartValue.cart.Items {
		product, productExists := r.state.products[cartItem.ProductID]
		if !productExists || product.Availability != catalog.AvailabilityAvailable {
			return nil, apperror.ErrPrecondition
		}
		if index == 0 {
			total, err = money.New(0, product.Price.Currency)
			if err != nil {
				return nil, err
			}
		}
		if product.Price.Currency != total.Currency {
			return nil, apperror.ErrPrecondition
		}
		lineTotal, multiplyErr := product.Price.Multiply(cartItem.Quantity)
		if multiplyErr != nil {
			return nil, multiplyErr
		}
		if err := order.CheckLineMinimum(product.Name, cartItem.Quantity, lineTotal, product.MinOrderQuantity, product.MinOrderAmount); err != nil {
			return nil, err
		}
		total, err = total.Add(lineTotal)
		if err != nil {
			return nil, err
		}
		items = append(items, order.Item{
			ID: formatUUID(uint64(10_000 + index)), ProductID: product.ID, ProductName: product.Name,
			UnitPrice: product.Price, Quantity: cartItem.Quantity, LineTotal: lineTotal,
			UnitOfMeasure: product.UnitOfMeasure,
			CreatedAt:     command.CreatedAt, UpdatedAt: command.CreatedAt,
		})
	}

	created := order.Order{
		ID: command.OrderID, Number: command.OrderNumber, Name: "Заказ " + command.OrderNumber,
		CartID: command.CartID, Customer: command.Customer, Delivery: command.Delivery,
		CustomerComment: command.CustomerComment, Status: order.StatusNew,
		Items: items, Total: total, Version: 1, CreatedAt: command.CreatedAt, UpdatedAt: command.CreatedAt,
	}
	r.state.orders[created.ID] = storedOrder{
		value: cloneOrder(created), tokenHash: append([]byte(nil), command.OrderTokenHash...),
		requestHash: append([]byte(nil), command.RequestHash...),
	}
	r.state.idempotency[command.IdempotencyKey] = created.ID
	cartValue.cart.Status = cart.StatusCheckedOut
	cartValue.cart.Version++
	cartValue.cart.UpdatedAt = command.CreatedAt
	r.state.carts[command.CartID] = cartValue
	r.appendEventLocked(created.ID, order.EventCreated, "", "", nil)

	result := cloneOrder(created)
	return &result, nil
}

func (r *memoryOrderRepository) ListDeliveryPlaces(context.Context) ([]order.DeliveryPlace, error) {
	return nil, nil
}

func (r *memoryOrderRepository) ListDeliveryDays(_ context.Context, _ string, from time.Time, days int) ([]order.DeliveryDay, error) {
	if days <= 0 {
		return nil, nil
	}
	result := make([]order.DeliveryDay, 0, days)
	for index := 0; index < days; index++ {
		result = append(result, order.DeliveryDay{Date: from.AddDate(0, 0, index), Limit: 20})
	}
	return result, nil
}

func (r *memoryOrderRepository) GetPublic(_ context.Context, number string, tokenHash []byte) (*order.Order, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	for _, stored := range r.state.orders {
		if stored.value.Number == number && bytes.Equal(stored.tokenHash, tokenHash) {
			result := cloneOrder(stored.value)
			return &result, nil
		}
	}
	return nil, apperror.ErrNotFound
}

func (r *memoryOrderRepository) appendEventLocked(
	orderID string,
	eventType order.EventType,
	actorID string,
	comment string,
	changes []order.FieldChange,
) {
	events := r.state.events[orderID]
	r.state.events[orderID] = append(events, order.Event{
		ID: formatUUID(uint64(20_000 + len(events))), OrderID: orderID, Type: eventType,
		ActorAdminID: actorID, Comment: comment, Changes: append([]order.FieldChange(nil), changes...),
		CreatedAt: r.clock.Now(),
	})
}

type memoryAdminOrderRepository struct {
	state *memoryState
	clock fixedClock
}

var _ order.AdminRepository = (*memoryAdminOrderRepository)(nil)

func (r *memoryAdminOrderRepository) List(_ context.Context, filter order.ListFilter) ([]order.Order, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	result := make([]order.Order, 0, len(r.state.orders))
	for _, stored := range r.state.orders {
		value := stored.value
		if !matchesOrderStatus(value.Status, filter.Statuses) || !matchesOrderQuery(value, filter.Query) {
			continue
		}
		if filter.CreatedFrom != nil && value.CreatedAt.Before(*filter.CreatedFrom) {
			continue
		}
		if filter.CreatedTo != nil && !value.CreatedAt.Before(*filter.CreatedTo) {
			continue
		}
		if filter.AssignedAdminID != "" && value.AssignedAdminID != filter.AssignedAdminID {
			continue
		}
		if filter.Cursor != nil && !beforeCursor(value.CreatedAt, value.ID, filter.Cursor) {
			continue
		}
		result = append(result, cloneOrder(value))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	if len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (r *memoryAdminOrderRepository) GetByID(_ context.Context, orderID string) (*order.Order, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	stored, exists := r.state.orders[orderID]
	if !exists {
		return nil, apperror.ErrNotFound
	}
	result := cloneOrder(stored.value)
	return &result, nil
}

func (r *memoryAdminOrderRepository) UpdateDetails(
	_ context.Context,
	orderID string,
	expectedVersion uint64,
	actorID string,
	patch order.DetailsPatch,
	_ []string,
) (*order.Order, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	stored, err := r.mutableOrderLocked(orderID, expectedVersion)
	if err != nil {
		return nil, err
	}
	changes := make([]order.FieldChange, 0, len(patch.Paths))
	for _, path := range patch.Paths {
		switch path {
		case "name":
			changes = append(changes, fieldChange(path, stored.value.Name, patch.Name))
			stored.value.Name = patch.Name
		case "customer":
			stored.value.Customer = patch.Customer
		case "delivery":
			stored.value.Delivery = patch.Delivery
		case "customer_comment":
			changes = append(changes, fieldChange(path, stored.value.CustomerComment, patch.CustomerComment))
			stored.value.CustomerComment = patch.CustomerComment
		case "internal_comment":
			changes = append(changes, fieldChange(path, stored.value.InternalComment, patch.InternalComment))
			stored.value.InternalComment = patch.InternalComment
		case "assigned_admin_id":
			changes = append(changes, fieldChange(path, stored.value.AssignedAdminID, patch.AssignedAdminID))
			stored.value.AssignedAdminID = patch.AssignedAdminID
		}
	}
	return r.finishMutationLocked(stored, actorID, order.EventDetailsChanged, "", changes)
}

func (r *memoryAdminOrderRepository) AddItem(
	_ context.Context,
	orderID string,
	expectedVersion uint64,
	actorID string,
	productID string,
	quantity uint32,
	priceOverride *money.Money,
	_ []string,
) (*order.Order, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	stored, err := r.mutableOrderLocked(orderID, expectedVersion)
	if err != nil {
		return nil, err
	}
	product, exists := r.state.products[productID]
	if !exists {
		return nil, apperror.ErrNotFound
	}
	if product.Availability == catalog.AvailabilityArchived {
		return nil, apperror.ErrPrecondition
	}
	unitPrice := product.Price
	if priceOverride != nil {
		unitPrice = *priceOverride
	}
	if unitPrice.Currency != stored.value.Total.Currency {
		return nil, apperror.ErrPrecondition
	}
	lineTotal, err := unitPrice.Multiply(quantity)
	if err != nil {
		return nil, err
	}
	item := order.Item{
		ID: formatUUID(uint64(30_000 + len(stored.value.Items))), ProductID: product.ID,
		ProductName: product.Name, UnitPrice: unitPrice, Quantity: quantity, LineTotal: lineTotal,
		UnitOfMeasure: product.UnitOfMeasure,
		CreatedAt:     r.clock.Now(), UpdatedAt: r.clock.Now(),
	}
	stored.value.Items = append(stored.value.Items, item)
	if err := recalculateOrderTotal(&stored.value); err != nil {
		return nil, err
	}
	return r.finishMutationLocked(stored, actorID, order.EventItemAdded, "", []order.FieldChange{
		fieldChange("items."+item.ID, "", item.ProductName),
	})
}

func (r *memoryAdminOrderRepository) UpdateItem(
	_ context.Context,
	orderID string,
	itemID string,
	expectedVersion uint64,
	actorID string,
	patch order.ItemPatch,
	_ []string,
) (*order.Order, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	stored, err := r.mutableOrderLocked(orderID, expectedVersion)
	if err != nil {
		return nil, err
	}
	itemIndex := -1
	for index := range stored.value.Items {
		if stored.value.Items[index].ID == itemID {
			itemIndex = index
			break
		}
	}
	if itemIndex < 0 {
		return nil, apperror.ErrNotFound
	}
	item := &stored.value.Items[itemIndex]
	for _, path := range patch.Paths {
		switch path {
		case "product_name":
			item.ProductName = patch.ProductName
		case "quantity":
			item.Quantity = patch.Quantity
		case "unit_price":
			item.UnitPrice = *patch.UnitPrice
		}
	}
	item.LineTotal, err = item.UnitPrice.Multiply(item.Quantity)
	if err != nil {
		return nil, err
	}
	item.UpdatedAt = r.clock.Now()
	if err := recalculateOrderTotal(&stored.value); err != nil {
		return nil, err
	}
	return r.finishMutationLocked(stored, actorID, order.EventItemChanged, "", nil)
}

func (r *memoryAdminOrderRepository) RemoveItem(
	_ context.Context,
	orderID string,
	itemID string,
	expectedVersion uint64,
	actorID string,
	_ []string,
) (*order.Order, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	stored, err := r.mutableOrderLocked(orderID, expectedVersion)
	if err != nil {
		return nil, err
	}
	if len(stored.value.Items) <= 1 {
		return nil, apperror.ErrPrecondition
	}
	for index, item := range stored.value.Items {
		if item.ID != itemID {
			continue
		}
		stored.value.Items = append(stored.value.Items[:index], stored.value.Items[index+1:]...)
		if err := recalculateOrderTotal(&stored.value); err != nil {
			return nil, err
		}
		return r.finishMutationLocked(stored, actorID, order.EventItemRemoved, "", []order.FieldChange{
			fieldChange("items."+item.ID, item.ProductName, ""),
		})
	}
	return nil, apperror.ErrNotFound
}

func (r *memoryAdminOrderRepository) ChangeStatus(
	_ context.Context,
	orderID string,
	expectedVersion uint64,
	actorID string,
	from order.Status,
	to order.Status,
	comment string,
	_ []string,
) (*order.Order, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	stored, err := r.mutableOrderLocked(orderID, expectedVersion)
	if err != nil {
		return nil, err
	}
	if stored.value.Status != from || !order.CanTransition(from, to) {
		return nil, apperror.ErrConflict
	}
	stored.value.Status = to
	return r.finishMutationLocked(stored, actorID, order.EventStatusChanged, comment, []order.FieldChange{
		fieldChange("status", string(from), string(to)),
	})
}

func (r *memoryAdminOrderRepository) SetDeliveryDayLimit(_ context.Context, date time.Time, limit int) (order.DeliveryDay, error) {
	return order.DeliveryDay{Date: date, Limit: limit}, nil
}

func (r *memoryAdminOrderRepository) CreateDeliveryPlace(_ context.Context, place order.DeliveryPlace) (order.DeliveryPlace, error) {
	return place, nil
}

func (r *memoryAdminOrderRepository) UpdateDeliveryPlace(_ context.Context, place order.DeliveryPlace) (order.DeliveryPlace, error) {
	return place, nil
}

func (r *memoryAdminOrderRepository) DeleteDeliveryPlace(context.Context, string) error {
	return nil
}

func (r *memoryAdminOrderRepository) ListEvents(
	_ context.Context,
	orderID string,
	cursor *pagination.Cursor,
	limit int,
) ([]order.Event, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	if _, exists := r.state.orders[orderID]; !exists {
		return nil, apperror.ErrNotFound
	}
	events := make([]order.Event, 0, len(r.state.events[orderID]))
	for _, event := range r.state.events[orderID] {
		if cursor != nil && !beforeCursor(event.CreatedAt, event.ID, cursor) {
			continue
		}
		event.Changes = append([]order.FieldChange(nil), event.Changes...)
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].ID > events[j].ID
		}
		return events[i].CreatedAt.After(events[j].CreatedAt)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func (r *memoryAdminOrderRepository) mutableOrderLocked(orderID string, expectedVersion uint64) (*storedOrder, error) {
	stored, exists := r.state.orders[orderID]
	if !exists {
		return nil, apperror.ErrNotFound
	}
	if stored.value.Version != expectedVersion {
		return nil, apperror.ErrConflict
	}
	return &stored, nil
}

func (r *memoryAdminOrderRepository) finishMutationLocked(
	stored *storedOrder,
	actorID string,
	eventType order.EventType,
	comment string,
	changes []order.FieldChange,
) (*order.Order, error) {
	stored.value.Version++
	stored.value.UpdatedAt = r.clock.Now()
	r.state.orders[stored.value.ID] = *stored
	events := r.state.events[stored.value.ID]
	r.state.events[stored.value.ID] = append(events, order.Event{
		ID: formatUUID(uint64(20_000 + len(events))), OrderID: stored.value.ID, Type: eventType,
		ActorAdminID: actorID, Comment: comment, Changes: append([]order.FieldChange(nil), changes...),
		CreatedAt: r.clock.Now(),
	})
	result := cloneOrder(stored.value)
	return &result, nil
}

func recalculateOrderTotal(value *order.Order) error {
	total, err := money.New(0, value.Total.Currency)
	if err != nil {
		return err
	}
	for _, item := range value.Items {
		total, err = total.Add(item.LineTotal)
		if err != nil {
			return err
		}
	}
	value.Total = total
	return nil
}

func cloneOrder(value order.Order) order.Order {
	value.Items = append([]order.Item(nil), value.Items...)
	return value
}

func matchesOrderStatus(value order.Status, requested []order.Status) bool {
	if len(requested) == 0 {
		return true
	}
	for _, current := range requested {
		if current == value {
			return true
		}
	}
	return false
}

func matchesOrderQuery(value order.Order, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value.Number), query) ||
		strings.Contains(strings.ToLower(value.Name), query) ||
		strings.Contains(strings.ToLower(value.Customer.FullName), query) ||
		strings.Contains(strings.ToLower(value.Customer.Phone), query)
}

func beforeCursor(createdAt time.Time, id string, cursor *pagination.Cursor) bool {
	return createdAt.Before(cursor.CreatedAt) || (createdAt.Equal(cursor.CreatedAt) && id < cursor.ID)
}

func fieldChange(path, oldValue, newValue string) order.FieldChange {
	return order.FieldChange{FieldPath: path, OldValue: oldValue, NewValue: newValue}
}

type memoryAuthRepository struct {
	mutex    sync.Mutex
	user     auth.User
	sessions map[string]auth.Session
}

var _ auth.Repository = (*memoryAuthRepository)(nil)

func (r *memoryAuthRepository) GetByLogin(_ context.Context, login string) (*auth.User, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if !strings.EqualFold(r.user.Login, login) {
		return nil, apperror.ErrNotFound
	}
	result := cloneUser(r.user)
	return &result, nil
}

func (r *memoryAuthRepository) CreateSession(_ context.Context, session auth.Session) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.sessions[string(session.RefreshTokenHash)] = cloneSession(session)
	return nil
}

func (r *memoryAuthRepository) RotateSession(
	_ context.Context,
	oldHash []byte,
	newHash []byte,
	expiresAt time.Time,
	now time.Time,
) (*auth.User, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	oldKey := string(oldHash)
	session, exists := r.sessions[oldKey]
	if !exists || !session.ExpiresAt.After(now) {
		return nil, apperror.ErrUnauthenticated
	}
	delete(r.sessions, oldKey)
	session.RefreshTokenHash = append([]byte(nil), newHash...)
	session.ExpiresAt = expiresAt
	r.sessions[string(newHash)] = session
	result := cloneUser(r.user)
	return &result, nil
}

func (r *memoryAuthRepository) RevokeSession(_ context.Context, tokenHash []byte, _ time.Time) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	key := string(tokenHash)
	if _, exists := r.sessions[key]; !exists {
		return apperror.ErrUnauthenticated
	}
	delete(r.sessions, key)
	return nil
}

func (r *memoryAuthRepository) CreateUser(_ context.Context, user auth.User) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if strings.EqualFold(r.user.Login, user.Login) {
		return apperror.ErrConflict
	}
	r.user = cloneUser(user)
	return nil
}

func cloneUser(user auth.User) auth.User {
	user.Roles = append([]auth.Role(nil), user.Roles...)
	return user
}

func cloneSession(session auth.Session) auth.Session {
	session.RefreshTokenHash = append([]byte(nil), session.RefreshTokenHash...)
	return session
}

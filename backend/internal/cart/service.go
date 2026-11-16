package cart

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/catalog"
	"github.com/SV1Stail/online_market/internal/identifier"
	"github.com/SV1Stail/online_market/internal/money"
)

const maxItemQuantity = 10_000

type Repository interface {
	Create(context.Context, Cart, []byte) error
	Get(context.Context, string, []byte) (*Cart, error)
	AddItem(context.Context, string, []byte, string, string, uint32, uint64) (*Cart, error)
	UpdateItem(context.Context, string, []byte, string, uint32, uint64) (*Cart, error)
	RemoveItem(context.Context, string, []byte, string, uint64) (*Cart, error)
	Clear(context.Context, string, []byte, uint64) (*Cart, error)
}

type ProductReader interface {
	GetProduct(context.Context, string) (*catalog.Product, error)
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	UUID() (string, error)
	Token(int) (string, error)
}

type CreateResult struct {
	Cart  *Cart
	Token string
}

type Service struct {
	repository Repository
	products   ProductReader
	clock      Clock
	ids        IDGenerator
	ttl        time.Duration
}

func NewService(repository Repository, products ProductReader, clock Clock, ids IDGenerator, ttl time.Duration) *Service {
	return &Service{repository: repository, products: products, clock: clock, ids: ids, ttl: ttl}
}

func (s *Service) Create(ctx context.Context, currency string) (*CreateResult, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "RUB"
	}
	if currency != "RUB" {
		return nil, apperror.Validation("currency_code", "only RUB is supported")
	}
	id, err := s.ids.UUID()
	if err != nil {
		return nil, fmt.Errorf("generate cart id: %w", err)
	}
	token, err := s.ids.Token(32)
	if err != nil {
		return nil, fmt.Errorf("generate cart token: %w", err)
	}
	now := s.clock.Now()
	zero, err := money.New(0, currency)
	if err != nil {
		return nil, fmt.Errorf("create cart total: %w", err)
	}
	cart := Cart{
		ID:        id,
		Status:    StatusOpen,
		Items:     []Item{},
		Total:     zero,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}
	if err := s.repository.Create(ctx, cart, identifier.HashToken(token)); err != nil {
		return nil, fmt.Errorf("create cart: %w", err)
	}
	return &CreateResult{Cart: &cart, Token: token}, nil
}

func (s *Service) Get(ctx context.Context, cartID, token string) (*Cart, error) {
	if err := validateAccess(cartID, token); err != nil {
		return nil, err
	}
	result, err := s.repository.Get(ctx, cartID, identifier.HashToken(token))
	if err != nil {
		return nil, fmt.Errorf("get cart: %w", err)
	}
	return result, nil
}

func (s *Service) AddItem(ctx context.Context, cartID, token, productID string, quantity uint32, expectedVersion uint64) (*Cart, error) {
	if err := validateAccess(cartID, token); err != nil {
		return nil, err
	}
	if !identifier.IsUUID(strings.TrimSpace(productID)) {
		return nil, apperror.Validation("product_id", "must be a valid UUID")
	}
	if err := validateQuantity(quantity); err != nil {
		return nil, err
	}
	product, err := s.products.GetProduct(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("get product for cart: %w", err)
	}
	if product.Availability != catalog.AvailabilityAvailable {
		return nil, fmt.Errorf("%w: product is not available", apperror.ErrPrecondition)
	}
	itemID, err := s.ids.UUID()
	if err != nil {
		return nil, fmt.Errorf("generate cart item id: %w", err)
	}
	result, err := s.repository.AddItem(
		ctx,
		cartID,
		identifier.HashToken(token),
		itemID,
		product.ID,
		quantity,
		expectedVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("add cart item: %w", err)
	}
	return result, nil
}

func (s *Service) UpdateItem(ctx context.Context, cartID, token, itemID string, quantity uint32, expectedVersion uint64) (*Cart, error) {
	if err := validateAccess(cartID, token); err != nil {
		return nil, err
	}
	if !identifier.IsUUID(strings.TrimSpace(itemID)) {
		return nil, apperror.Validation("item_id", "must be a valid UUID")
	}
	if err := validateQuantity(quantity); err != nil {
		return nil, err
	}
	result, err := s.repository.UpdateItem(ctx, cartID, identifier.HashToken(token), itemID, quantity, expectedVersion)
	if err != nil {
		return nil, fmt.Errorf("update cart item: %w", err)
	}
	return result, nil
}

func (s *Service) RemoveItem(ctx context.Context, cartID, token, itemID string, expectedVersion uint64) (*Cart, error) {
	if err := validateAccess(cartID, token); err != nil {
		return nil, err
	}
	if !identifier.IsUUID(strings.TrimSpace(itemID)) {
		return nil, apperror.Validation("item_id", "must be a valid UUID")
	}
	result, err := s.repository.RemoveItem(ctx, cartID, identifier.HashToken(token), itemID, expectedVersion)
	if err != nil {
		return nil, fmt.Errorf("remove cart item: %w", err)
	}
	return result, nil
}

func (s *Service) Clear(ctx context.Context, cartID, token string, expectedVersion uint64) (*Cart, error) {
	if err := validateAccess(cartID, token); err != nil {
		return nil, err
	}
	result, err := s.repository.Clear(ctx, cartID, identifier.HashToken(token), expectedVersion)
	if err != nil {
		return nil, fmt.Errorf("clear cart: %w", err)
	}
	return result, nil
}

func validateAccess(cartID, token string) error {
	if !identifier.IsUUID(strings.TrimSpace(cartID)) {
		return apperror.Validation("cart_id", "must be a valid UUID")
	}
	if !identifier.IsToken(token, 32) {
		return apperror.ErrUnauthenticated
	}
	return nil
}

func validateQuantity(quantity uint32) error {
	if quantity == 0 || quantity > maxItemQuantity {
		return apperror.Validation("quantity", "must be between 1 and 10000")
	}
	return nil
}

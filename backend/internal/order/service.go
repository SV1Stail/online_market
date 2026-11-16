package order

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/identifier"
)

type CheckoutCommand struct {
	OrderID              string
	OrderNumber          string
	CartID               string
	CartTokenHash        []byte
	OrderTokenHash       []byte
	IdempotencyKey       string
	RequestHash          []byte
	Customer             Customer
	Delivery             Delivery
	CustomerComment      string
	NotificationChannels []string
	CreatedAt            time.Time
}

type Repository interface {
	Create(context.Context, CheckoutCommand) (*Order, error)
	GetPublic(context.Context, string, []byte) (*Order, error)
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	UUID() (string, error)
}

type TokenDeriver interface {
	Derive(string) string
}

type CreateInput struct {
	CartID          string
	CartToken       string
	Customer        Customer
	Delivery        Delivery
	CustomerComment string
	IdempotencyKey  string
}

type CreateResult struct {
	Order *Order
	Token string
}

type Service struct {
	repository           Repository
	clock                Clock
	ids                  IDGenerator
	tokens               TokenDeriver
	notificationChannels []string
}

func NewService(repository Repository, clock Clock, ids IDGenerator, tokens TokenDeriver, notificationChannels []string) *Service {
	return &Service{
		repository:           repository,
		clock:                clock,
		ids:                  ids,
		tokens:               tokens,
		notificationChannels: append([]string(nil), notificationChannels...),
	}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*CreateResult, error) {
	input = normalizeCreateInput(input)
	if err := validateCreateInput(input); err != nil {
		return nil, err
	}
	requestHash, err := checkoutHash(input)
	if err != nil {
		return nil, fmt.Errorf("hash checkout request: %w", err)
	}
	orderID, err := s.ids.UUID()
	if err != nil {
		return nil, fmt.Errorf("generate order id: %w", err)
	}
	now := s.clock.Now()
	number := fmt.Sprintf("ORD-%s-%s", now.Format("20060102"), strings.ToUpper(strings.ReplaceAll(orderID[:8], "-", "")))
	orderToken := s.tokens.Derive("order:" + orderID)

	created, err := s.repository.Create(ctx, CheckoutCommand{
		OrderID:              orderID,
		OrderNumber:          number,
		CartID:               input.CartID,
		CartTokenHash:        identifier.HashToken(input.CartToken),
		OrderTokenHash:       identifier.HashToken(orderToken),
		IdempotencyKey:       input.IdempotencyKey,
		RequestHash:          requestHash,
		Customer:             input.Customer,
		Delivery:             input.Delivery,
		CustomerComment:      input.CustomerComment,
		NotificationChannels: s.notificationChannels,
		CreatedAt:            now,
	})
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	// An idempotent replay returns the persisted order ID, so derive its token,
	// not the unused ID generated for this invocation.
	orderToken = s.tokens.Derive("order:" + created.ID)
	return &CreateResult{Order: created, Token: orderToken}, nil
}

func (s *Service) Get(ctx context.Context, number, token string) (*Order, error) {
	if strings.TrimSpace(number) == "" || len(number) > 64 {
		return nil, apperror.Validation("order_number", "must contain between 1 and 64 characters")
	}
	if !identifier.IsToken(token, 32) {
		return nil, apperror.ErrUnauthenticated
	}
	result, err := s.repository.GetPublic(ctx, number, identifier.HashToken(token))
	if err != nil {
		return nil, fmt.Errorf("get public order: %w", err)
	}
	return result, nil
}

func normalizeCreateInput(input CreateInput) CreateInput {
	input.CartID = strings.TrimSpace(input.CartID)
	input.Customer.FullName = strings.TrimSpace(input.Customer.FullName)
	input.Customer.Phone = strings.TrimSpace(input.Customer.Phone)
	input.Customer.Email = strings.TrimSpace(input.Customer.Email)
	input.Delivery.City = strings.TrimSpace(input.Delivery.City)
	input.Delivery.Address = strings.TrimSpace(input.Delivery.Address)
	input.Delivery.Comment = strings.TrimSpace(input.Delivery.Comment)
	input.CustomerComment = strings.TrimSpace(input.CustomerComment)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	return input
}

func validateCreateInput(input CreateInput) error {
	if !identifier.IsUUID(input.CartID) {
		return apperror.Validation("cart_id", "must be a valid UUID")
	}
	if !identifier.IsToken(input.CartToken, 32) {
		return apperror.ErrUnauthenticated
	}
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return apperror.Validation("idempotency_key", "must contain between 1 and 128 characters")
	}
	if len(input.CustomerComment) > 4000 {
		return apperror.Validation("customer_comment", "must not exceed 4000 characters")
	}
	if err := validateCustomer(input.Customer); err != nil {
		return err
	}
	if err := validateDelivery(input.Delivery); err != nil {
		return err
	}
	return nil
}

func validateCustomer(customer Customer) error {
	customer.FullName = strings.TrimSpace(customer.FullName)
	customer.Phone = strings.TrimSpace(customer.Phone)
	customer.Email = strings.TrimSpace(customer.Email)
	if customer.FullName == "" || len(customer.FullName) > 255 {
		return apperror.Validation("customer.full_name", "must contain between 1 and 255 characters")
	}
	if customer.Phone == "" || len(customer.Phone) > 64 {
		return apperror.Validation("customer.phone", "must contain between 1 and 64 characters")
	}
	if customer.Email != "" {
		if len(customer.Email) > 320 {
			return apperror.Validation("customer.email", "must not exceed 320 characters")
		}
		address, err := mail.ParseAddress(customer.Email)
		if err != nil || address.Address != customer.Email {
			return apperror.Validation("customer.email", "must be a valid email address")
		}
	}
	return nil
}

func validateDelivery(delivery Delivery) error {
	delivery.City = strings.TrimSpace(delivery.City)
	delivery.Address = strings.TrimSpace(delivery.Address)
	if len(delivery.City) > 255 {
		return apperror.Validation("delivery.city", "must not exceed 255 characters")
	}
	if len(delivery.Comment) > 4000 {
		return apperror.Validation("delivery.comment", "must not exceed 4000 characters")
	}
	switch delivery.Method {
	case DeliveryPickup:
	case DeliveryCourier:
		if delivery.City == "" || delivery.Address == "" {
			return apperror.Validation("delivery", "city and address are required for courier delivery")
		}
	default:
		return apperror.Validation("delivery.method", "unsupported delivery method")
	}
	return nil
}

func checkoutHash(input CreateInput) ([]byte, error) {
	payload := struct {
		CartID          string   `json:"cart_id"`
		Customer        Customer `json:"customer"`
		Delivery        Delivery `json:"delivery"`
		CustomerComment string   `json:"customer_comment"`
	}{
		CartID: input.CartID, Customer: input.Customer, Delivery: input.Delivery, CustomerComment: input.CustomerComment,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	return hash[:], nil
}

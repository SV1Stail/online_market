package order

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type checkoutRepositoryStub struct {
	command *CheckoutCommand
	result  *Order
}

func (s *checkoutRepositoryStub) Create(_ context.Context, command CheckoutCommand) (*Order, error) {
	s.command = &command
	return s.result, nil
}

func (s *checkoutRepositoryStub) GetPublic(context.Context, string, []byte) (*Order, error) {
	return nil, nil
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type fixedIDs struct {
	id string
}

func (g fixedIDs) UUID() (string, error) {
	return g.id, nil
}

type visibleTokens struct{}

func (visibleTokens) Derive(purpose string) string {
	return "derived-" + purpose
}

func TestCreateUsesPersistedOrderIDForIdempotentToken(t *testing.T) {
	repository := &checkoutRepositoryStub{
		result: &Order{ID: "77777777-7777-4777-8777-777777777777"},
	}
	service := NewService(
		repository,
		fixedClock{now: time.Date(2026, time.November, 13, 12, 0, 0, 0, time.UTC)},
		fixedIDs{id: "11111111-1111-4111-8111-111111111111"},
		visibleTokens{},
		[]string{"telegram"},
	)

	result, err := service.Create(context.Background(), validCreateInput())
	require.NoError(t, err)
	require.Equal(t, "derived-order:77777777-7777-4777-8777-777777777777", result.Token)
	require.Equal(t, []string{"telegram"}, repository.command.NotificationChannels)
	require.NotEmpty(t, repository.command.RequestHash)
}

func TestCreateRejectsCourierWithoutAddress(t *testing.T) {
	service := NewService(
		&checkoutRepositoryStub{},
		fixedClock{},
		fixedIDs{id: "11111111-1111-4111-8111-111111111111"},
		visibleTokens{},
		nil,
	)
	input := validCreateInput()
	input.Delivery = Delivery{Method: DeliveryCourier, City: "Moscow"}

	_, err := service.Create(context.Background(), input)
	require.Error(t, err)
}

func validCreateInput() CreateInput {
	return CreateInput{
		CartID:         "22222222-2222-4222-8222-222222222222",
		CartToken:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Customer:       Customer{FullName: "Customer", Phone: "+70000000000", Email: "customer@example.com"},
		Delivery:       Delivery{Method: DeliveryPickup},
		IdempotencyKey: "33333333-3333-4333-8333-333333333333",
	}
}

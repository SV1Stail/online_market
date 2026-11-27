package grpcapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	marketv1 "github.com/SV1Stail/online_market/gen/online_market/v1"
	"github.com/SV1Stail/online_market/internal/ratelimit"
)

func TestRateLimitPolicies(t *testing.T) {
	policies := RateLimitPolicies()
	standardRules := []ratelimit.Rule{
		{Limit: 100, Window: time.Minute},
		{Limit: 30, Window: 10 * time.Second},
	}

	require.Equal(t, ratelimit.Policy{Name: "catalog", Rules: standardRules}, policies[marketv1.CatalogService_ListProducts_FullMethodName])
	require.Equal(t, ratelimit.Policy{Name: "cart", Rules: standardRules}, policies[marketv1.CartService_AddCartItem_FullMethodName])
	require.Equal(t, ratelimit.Policy{Name: "create_order", Rules: standardRules}, policies[marketv1.OrderService_CreateOrder_FullMethodName])
	require.Equal(t, ratelimit.Policy{Name: "delivery_days", Rules: standardRules}, policies[marketv1.OrderService_ListDeliveryDays_FullMethodName])
	require.Equal(t, ratelimit.Policy{Name: "delivery_places", Rules: standardRules}, policies[marketv1.OrderService_ListDeliveryPlaces_FullMethodName])
	require.Equal(t, ratelimit.Policy{
		Name:  "refresh_session",
		Rules: []ratelimit.Rule{{Limit: 10, Window: time.Minute}},
	}, policies[marketv1.AdminAuthService_RefreshSession_FullMethodName])
	require.NotContains(t, policies, marketv1.AdminOrderService_GetOrder_FullMethodName)
	require.NotContains(t, policies, marketv1.OrderService_GetOrder_FullMethodName)
}

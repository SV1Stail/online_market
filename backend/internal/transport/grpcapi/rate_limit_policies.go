package grpcapi

import (
	"time"

	marketv1 "github.com/SV1Stail/online_market/gen/online_market/v1"
	"github.com/SV1Stail/online_market/internal/ratelimit"
)

func RateLimitPolicies() map[string]ratelimit.Policy {
	standardRules := []ratelimit.Rule{
		{Limit: 100, Window: time.Minute},
		{Limit: 30, Window: 10 * time.Second},
	}
	policies := make(map[string]ratelimit.Policy)
	addPolicy(policies, "catalog", standardRules,
		marketv1.CatalogService_ListProducts_FullMethodName,
		marketv1.CatalogService_GetProduct_FullMethodName,
		marketv1.CatalogService_ListCategories_FullMethodName,
	)
	addPolicy(policies, "cart", standardRules,
		marketv1.CartService_CreateCart_FullMethodName,
		marketv1.CartService_GetCart_FullMethodName,
		marketv1.CartService_AddCartItem_FullMethodName,
		marketv1.CartService_UpdateCartItem_FullMethodName,
		marketv1.CartService_RemoveCartItem_FullMethodName,
		marketv1.CartService_ClearCart_FullMethodName,
	)
	addPolicy(policies, "create_order", standardRules, marketv1.OrderService_CreateOrder_FullMethodName)
	addPolicy(policies, "delivery_days", standardRules, marketv1.OrderService_ListDeliveryDays_FullMethodName)
	addPolicy(policies, "delivery_places", standardRules, marketv1.OrderService_ListDeliveryPlaces_FullMethodName)
	addPolicy(policies, "refresh_session", []ratelimit.Rule{
		{Limit: 10, Window: time.Minute},
	}, marketv1.AdminAuthService_RefreshSession_FullMethodName)
	return policies
}

func addPolicy(policies map[string]ratelimit.Policy, name string, rules []ratelimit.Rule, methods ...string) {
	for _, method := range methods {
		policies[method] = ratelimit.Policy{Name: name, Rules: rules}
	}
}

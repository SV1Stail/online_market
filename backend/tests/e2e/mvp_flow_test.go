package e2e_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	marketv1 "github.com/SV1Stail/online_market/gen/online_market/v1"
	"github.com/SV1Stail/online_market/internal/auth"
	"github.com/SV1Stail/online_market/internal/cart"
	"github.com/SV1Stail/online_market/internal/catalog"
	"github.com/SV1Stail/online_market/internal/order"
	"github.com/SV1Stail/online_market/internal/ratelimit"
	"github.com/SV1Stail/online_market/internal/security"
	"github.com/SV1Stail/online_market/internal/transport/grpcapi"
)

func TestMVPFlow(t *testing.T) {
	environment := newEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	catalogClient := marketv1.NewCatalogServiceClient(environment.connection)
	cartClient := marketv1.NewCartServiceClient(environment.connection)
	orderClient := marketv1.NewOrderServiceClient(environment.connection)
	authClient := marketv1.NewAdminAuthServiceClient(environment.connection)
	adminOrderClient := marketv1.NewAdminOrderServiceClient(environment.connection)
	adminCatalogClient := marketv1.NewAdminCatalogServiceClient(environment.connection)

	products, err := catalogClient.ListProducts(ctx, &marketv1.ListProductsRequest{OnlyAvailable: true})
	require.NoError(t, err)
	require.NotEmpty(t, products.GetProducts())
	seedProduct := products.GetProducts()[0]

	createdCart, err := cartClient.CreateCart(ctx, &marketv1.CreateCartRequest{CurrencyCode: "RUB"})
	require.NoError(t, err)
	require.NotEmpty(t, createdCart.GetCartToken())

	_, err = cartClient.AddCartItem(ctx, &marketv1.AddCartItemRequest{
		CartId: createdCart.GetCart().GetId(), CartToken: tokenFor(999),
		ProductId: seedProduct.GetId(), Quantity: 1,
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	updatedCart, err := cartClient.AddCartItem(ctx, &marketv1.AddCartItemRequest{
		CartId: createdCart.GetCart().GetId(), CartToken: createdCart.GetCartToken(),
		ProductId: seedProduct.GetId(), Quantity: 2,
	})
	require.NoError(t, err)
	require.Len(t, updatedCart.GetCart().GetItems(), 1)

	createOrderRequest := &marketv1.CreateOrderRequest{
		CartId: createdCart.GetCart().GetId(), CartToken: createdCart.GetCartToken(),
		Customer: &marketv1.CustomerContact{FullName: "Покупатель", Phone: "+79990000000"},
		Delivery: &marketv1.DeliveryDetails{
			Method: marketv1.DeliveryMethod_DELIVERY_METHOD_COURIER, City: "Якутск", Address: "Ленина, 1",
			DeliveryDate: "2026-11-16",
		},
		IdempotencyKey: "30000000-0000-4000-8000-000000000001",
	}
	createdOrder, err := orderClient.CreateOrder(ctx, createOrderRequest)
	require.NoError(t, err)
	require.Equal(t, marketv1.OrderStatus_ORDER_STATUS_NEW, createdOrder.GetOrder().GetStatus())

	replayedOrder, err := orderClient.CreateOrder(ctx, createOrderRequest)
	require.NoError(t, err)
	require.Equal(t, createdOrder.GetOrder().GetId(), replayedOrder.GetOrder().GetId())
	require.Equal(t, createdOrder.GetOrderToken(), replayedOrder.GetOrderToken())

	conflictingRequest := *createOrderRequest
	conflictingRequest.Customer = &marketv1.CustomerContact{FullName: "Другой покупатель", Phone: "+79991111111"}
	_, err = orderClient.CreateOrder(ctx, &conflictingRequest)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))

	publicOrder, err := orderClient.GetOrder(ctx, &marketv1.GetOrderRequest{
		OrderNumber: createdOrder.GetOrder().GetNumber(), OrderToken: createdOrder.GetOrderToken(),
	})
	require.NoError(t, err)
	require.Equal(t, uint32(2), publicOrder.GetOrder().GetItems()[0].GetQuantity())

	_, err = adminOrderClient.ListOrders(ctx, &marketv1.ListAdminOrdersRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	_, err = authClient.Login(ctx, &marketv1.LoginRequest{Login: "admin", Password: "wrong-password"})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	login, err := authClient.Login(ctx, &marketv1.LoginRequest{Login: "admin", Password: "strong-admin-password"})
	require.NoError(t, err)
	adminContext := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+login.GetAccessToken())

	adminOrders, err := adminOrderClient.ListOrders(adminContext, &marketv1.ListAdminOrdersRequest{})
	require.NoError(t, err)
	require.Len(t, adminOrders.GetOrders(), 1)

	renamedOrder, err := adminOrderClient.UpdateOrderDetails(adminContext, &marketv1.UpdateOrderDetailsRequest{
		OrderId:         createdOrder.GetOrder().GetId(),
		Patch:           &marketv1.OrderDetailsPatch{Name: "Приоритетный заказ"},
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"name"}},
		ExpectedVersion: adminOrders.GetOrders()[0].GetOrder().GetVersion(),
	})
	require.NoError(t, err)
	require.Equal(t, "Приоритетный заказ", renamedOrder.GetOrder().GetOrder().GetName())

	confirmedOrder, err := adminOrderClient.ChangeOrderStatus(adminContext, &marketv1.ChangeOrderStatusRequest{
		OrderId:         createdOrder.GetOrder().GetId(),
		Status:          marketv1.OrderStatus_ORDER_STATUS_CONFIRMED,
		ExpectedVersion: renamedOrder.GetOrder().GetOrder().GetVersion(),
	})
	require.NoError(t, err)
	require.Equal(t, marketv1.OrderStatus_ORDER_STATUS_CONFIRMED, confirmedOrder.GetOrder().GetOrder().GetStatus())

	category, err := adminCatalogClient.CreateCategory(adminContext, &marketv1.CreateCategoryRequest{
		Name: "Новая категория", SortOrder: 30,
	})
	require.NoError(t, err)

	product, err := adminCatalogClient.CreateProduct(adminContext, &marketv1.CreateProductRequest{
		Name: "Новый товар", Description: "Товар из e2e-сценария",
		Price:        &marketv1.Money{MinorUnits: 100_00, CurrencyCode: "RUB"},
		Availability: marketv1.ProductAvailability_PRODUCT_AVAILABILITY_AVAILABLE,
		CategoryIds:  []string{category.GetCategory().GetId()},
	})
	require.NoError(t, err)

	updatedProduct, err := adminCatalogClient.UpdateProduct(adminContext, &marketv1.UpdateProductRequest{
		ProductId:       product.GetProduct().GetId(),
		Patch:           &marketv1.ProductPatch{Price: &marketv1.Money{MinorUnits: 120_00, CurrencyCode: "RUB"}},
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"price"}},
		ExpectedVersion: product.GetProduct().GetVersion(),
	})
	require.NoError(t, err)
	require.Equal(t, int64(120_00), updatedProduct.GetProduct().GetPrice().GetMinorUnits())

	_, err = adminCatalogClient.UpdateProduct(adminContext, &marketv1.UpdateProductRequest{
		ProductId:       product.GetProduct().GetId(),
		Patch:           &marketv1.ProductPatch{Price: &marketv1.Money{MinorUnits: 130_00, CurrencyCode: "RUB"}},
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"price"}},
		ExpectedVersion: product.GetProduct().GetVersion(),
	})
	require.Equal(t, codes.Aborted, status.Code(err))

	deletedProduct, err := adminCatalogClient.DeleteProduct(adminContext, &marketv1.DeleteProductRequest{
		ProductId: updatedProduct.GetProduct().GetId(), ExpectedVersion: updatedProduct.GetProduct().GetVersion(),
	})
	require.NoError(t, err)
	require.Equal(t, marketv1.ProductAvailability_PRODUCT_AVAILABILITY_ARCHIVED, deletedProduct.GetProduct().GetAvailability())

	_, err = catalogClient.GetProduct(ctx, &marketv1.GetProductRequest{ProductId: deletedProduct.GetProduct().GetId()})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestAdminSessionLifecycle(t *testing.T) {
	environment := newEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := marketv1.NewAdminAuthServiceClient(environment.connection)
	login, err := client.Login(ctx, &marketv1.LoginRequest{
		Login: "admin", Password: "strong-admin-password",
	})
	require.NoError(t, err)

	refreshed, err := client.RefreshSession(ctx, &marketv1.RefreshSessionRequest{
		RefreshToken: login.GetRefreshToken(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, refreshed.GetAccessToken())
	require.NotEqual(t, login.GetRefreshToken(), refreshed.GetRefreshToken())

	_, err = client.RefreshSession(ctx, &marketv1.RefreshSessionRequest{
		RefreshToken: login.GetRefreshToken(),
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	_, err = client.Logout(ctx, &marketv1.LogoutRequest{RefreshToken: refreshed.GetRefreshToken()})
	require.NoError(t, err)

	_, err = client.RefreshSession(ctx, &marketv1.RefreshSessionRequest{
		RefreshToken: refreshed.GetRefreshToken(),
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

type environment struct {
	connection *grpc.ClientConn
}

func newEnvironment(t *testing.T) *environment {
	t.Helper()
	now := time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{now: now}
	ids := &sequenceIDs{}
	state := newMemoryState(t, now)
	catalogRepository := &memoryCatalogRepository{state: state, clock: clock}
	cartRepository := &memoryCartRepository{state: state, clock: clock}
	orderRepository := &memoryOrderRepository{state: state, clock: clock}
	adminOrderRepository := &memoryAdminOrderRepository{state: state, clock: clock}
	authRepository := newMemoryAuthRepository(t)

	catalogService := catalog.NewService(catalogRepository)
	adminCatalogService := catalog.NewAdminService(catalogRepository, ids)
	cartService := cart.NewService(cartRepository, catalogService, clock, ids, 24*time.Hour)
	orderService := order.NewService(
		orderRepository, clock, ids,
		security.NewCapabilityTokens("e2e-order-token-secret-at-least-32-bytes"), nil,
	)
	adminOrderService := order.NewAdminService(adminOrderRepository, nil, clock)
	signer, err := security.NewSigner("e2e-access-token-secret-at-least-32-bytes")
	require.NoError(t, err)
	authService := auth.NewService(authRepository, clock, ids, signer, 15*time.Minute, 24*time.Hour)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	server := grpc.NewServer(grpc.ChainUnaryInterceptor(
		grpcapi.RequestIDInterceptor(logger),
		grpcapi.StructuredLoggingInterceptor(logger),
		grpcapi.RecoveryInterceptor(logger),
		grpcapi.DeadlineInterceptor(5*time.Second),
		grpcapi.RateLimitInterceptor(
			allowLimiter{}, staticIPResolver{address: netip.MustParseAddr("203.0.113.10")},
			grpcapi.RateLimitPolicies(), logger,
		),
		grpcapi.AuthenticationInterceptor(signer, clock, logger),
		grpcapi.AuthorizationInterceptor(),
		grpcapi.ErrorMappingInterceptor(logger),
	))
	marketv1.RegisterCatalogServiceServer(server, grpcapi.NewCatalogServer(catalogService))
	marketv1.RegisterCartServiceServer(server, grpcapi.NewCartServer(cartService))
	marketv1.RegisterOrderServiceServer(server, grpcapi.NewOrderServer(orderService))
	marketv1.RegisterAdminAuthServiceServer(server, grpcapi.NewAdminAuthServer(authService))
	marketv1.RegisterAdminCatalogServiceServer(server, grpcapi.NewAdminCatalogServer(adminCatalogService))
	marketv1.RegisterAdminOrderServiceServer(server, grpcapi.NewAdminOrderServer(adminOrderService))
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)

	listener := bufconn.Listen(1024 * 1024)
	go func() { _ = server.Serve(listener) }()
	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = connection.Close()
		server.Stop()
		_ = listener.Close()
	})
	return &environment{connection: connection}
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type allowLimiter struct{}

func (allowLimiter) Allow(context.Context, string, ratelimit.Policy) (*ratelimit.Decision, error) {
	return &ratelimit.Decision{Allowed: true}, nil
}

type staticIPResolver struct {
	address netip.Addr
}

func (r staticIPResolver) Resolve(context.Context) (netip.Addr, error) {
	return r.address, nil
}

type sequenceIDs struct {
	mutex sync.Mutex
	next  uint64
}

func (g *sequenceIDs) UUID() (string, error) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.next++
	return formatUUID(g.next), nil
}

func (g *sequenceIDs) Token(_ int) (string, error) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.next++
	return tokenFor(g.next), nil
}

func newMemoryAuthRepository(t *testing.T) *memoryAuthRepository {
	t.Helper()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("strong-admin-password"), bcrypt.MinCost)
	require.NoError(t, err)
	return &memoryAuthRepository{
		user: auth.User{
			ID: "40000000-0000-4000-8000-000000000001", Login: "admin",
			PasswordHash: string(passwordHash), DisplayName: "Administrator",
			Roles: []auth.Role{auth.RoleAdmin}, Active: true,
		},
		sessions: make(map[string]auth.Session),
	}
}

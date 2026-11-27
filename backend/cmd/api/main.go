package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	marketv1 "github.com/SV1Stail/online_market/gen/online_market/v1"
	"github.com/SV1Stail/online_market/internal/auth"
	"github.com/SV1Stail/online_market/internal/cart"
	"github.com/SV1Stail/online_market/internal/catalog"
	"github.com/SV1Stail/online_market/internal/clock"
	"github.com/SV1Stail/online_market/internal/config"
	"github.com/SV1Stail/online_market/internal/identifier"
	"github.com/SV1Stail/online_market/internal/notification"
	"github.com/SV1Stail/online_market/internal/order"
	platformpostgres "github.com/SV1Stail/online_market/internal/platform/postgres"
	platformredis "github.com/SV1Stail/online_market/internal/platform/redis"
	"github.com/SV1Stail/online_market/internal/ratelimit"
	"github.com/SV1Stail/online_market/internal/security"
	"github.com/SV1Stail/online_market/internal/transport/grpcapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	configValue, err := config.Load()
	if err != nil {
		return err
	}
	rootContext, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	pool, err := platformpostgres.Open(rootContext, configValue.DatabaseURL, logger)
	if err != nil {
		return err
	}
	defer pool.Close()
	redisClient, err := platformredis.Open(rootContext, configValue.RedisURL, logger)
	if err != nil {
		return err
	}
	defer redisClient.Close()

	signer, err := security.NewSigner(configValue.AccessTokenSecret)
	if err != nil {
		return err
	}
	systemClock := clock.System{}
	idGenerator := identifier.Generator{}

	catalogRepository := catalog.NewPostgresRepository(pool)
	if err := seedCatalog(rootContext, logger, catalogRepository, configValue.CatalogSeedFile); err != nil {
		return err
	}
	catalogService := catalog.NewService(catalogRepository)
	adminCatalogService := catalog.NewAdminService(catalogRepository, idGenerator)
	cartRepository := cart.NewPostgresRepository(pool)
	cartService := cart.NewService(cartRepository, catalogService, systemClock, idGenerator, configValue.CartTTL)
	orderRepository := order.NewPostgresRepository(pool, configValue.DeliveryDailyLimit, configValue.MinOrderTotalMinor)
	orderService := order.NewService(
		orderRepository,
		systemClock,
		idGenerator,
		security.NewCapabilityTokens(configValue.OrderTokenSecret),
		configValue.NotificationChannels,
	)
	adminOrderService := order.NewAdminService(orderRepository, configValue.NotificationChannels, systemClock)
	authRepository := auth.NewPostgresRepository(pool)
	authService := auth.NewService(
		authRepository,
		systemClock,
		idGenerator,
		signer,
		configValue.AccessTokenTTL,
		configValue.RefreshTokenTTL,
	)

	serverOptions := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			grpcapi.RequestIDInterceptor(logger),
			grpcapi.StructuredLoggingInterceptor(logger),
			grpcapi.RecoveryInterceptor(logger),
			grpcapi.DeadlineInterceptor(5*time.Second),
			grpcapi.RateLimitInterceptor(
				ratelimit.NewRedisLimiter(redisClient, "online_market:rate_limit"),
				grpcapi.NewClientIPResolver(configValue.TrustedProxyCIDRs),
				grpcapi.RateLimitPolicies(),
				logger,
			),
			grpcapi.AuthenticationInterceptor(signer, systemClock, logger),
			grpcapi.AuthorizationInterceptor(),
			grpcapi.ErrorMappingInterceptor(logger),
		),
	}
	if configValue.TLSCertificateFile != "" {
		transportCredentials, err := credentials.NewServerTLSFromFile(
			configValue.TLSCertificateFile,
			configValue.TLSPrivateKeyFile,
		)
		if err != nil {
			return err
		}
		serverOptions = append(serverOptions, grpc.Creds(transportCredentials))
	}
	grpcServer := grpc.NewServer(serverOptions...)
	marketv1.RegisterCatalogServiceServer(grpcServer, grpcapi.NewCatalogServer(catalogService))
	marketv1.RegisterAdminCatalogServiceServer(grpcServer, grpcapi.NewAdminCatalogServer(adminCatalogService))
	marketv1.RegisterCartServiceServer(grpcServer, grpcapi.NewCartServer(cartService))
	marketv1.RegisterOrderServiceServer(grpcServer, grpcapi.NewOrderServer(orderService))
	marketv1.RegisterAdminAuthServiceServer(grpcServer, grpcapi.NewAdminAuthServer(authService))
	marketv1.RegisterAdminOrderServiceServer(grpcServer, grpcapi.NewAdminOrderServer(adminOrderService))
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	listener, err := net.Listen("tcp", configValue.GRPCAddress)
	if err != nil {
		return err
	}
	defer listener.Close()

	workerDone := make(chan error, 1)
	if len(configValue.NotificationChannels) > 0 {
		senders := map[string]notification.Sender{
			"telegram": notification.NewTelegramSender(configValue.TelegramBotToken, configValue.TelegramChatID, logger),
		}
		worker := notification.NewWorker(
			notification.NewPostgresRepository(pool),
			senders,
			systemClock,
			logger,
			configValue.NotificationPollInterval,
		)
		go func() { workerDone <- worker.Run(rootContext) }()
	}

	serverDone := make(chan error, 1)
	go func() { serverDone <- grpcServer.Serve(listener) }()
	logger.Info("gRPC API started", "address", configValue.GRPCAddress)

	select {
	case <-rootContext.Done():
	case err := <-serverDone:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			cancel()
			return err
		}
	case err := <-workerDone:
		if err != nil {
			cancel()
			return err
		}
	}

	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	gracefulDone := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(gracefulDone)
	}()
	select {
	case <-gracefulDone:
	case <-time.After(10 * time.Second):
		grpcServer.Stop()
	}
	return nil
}

func seedCatalog(ctx context.Context, logger *slog.Logger, repository *catalog.PostgresRepository, path string) error {
	if path == "" {
		return nil
	}
	document, err := catalog.ParseSeedFile(path)
	if err != nil {
		catalog.LogSeedError(logger, err)
		return err
	}
	result, err := repository.ApplyBootstrap(ctx, document)
	if err != nil {
		catalog.LogSeedError(logger, err)
		return err
	}
	if !result.Applied {
		logger.Info("catalog seed skipped", "reason", "catalog is not empty")
		return nil
	}
	logger.Info("catalog seed applied", "categories", result.Categories, "products", result.Products)
	return nil
}

package grpcapi

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/SV1Stail/online_market/internal/observability"
	"github.com/SV1Stail/online_market/internal/security"
)

type principalKey struct{}

type Principal struct {
	UserID string
	Roles  []string
}

type tokenVerifier interface {
	Verify(string, time.Time) (*security.Claims, error)
}

type timeSource interface {
	Now() time.Time
}

const (
	adminOrderMethodPrefix   = "/online_market.api.v1.AdminOrderService/"
	adminCatalogMethodPrefix = "/online_market.api.v1.AdminCatalogService/"
)

func AuthenticationInterceptor(verifier tokenVerifier, clock timeSource, logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !requiresAdminIdentity(info.FullMethod) {
			return handler(ctx, request)
		}
		metadataValues, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "authorization metadata is required")
		}
		authorization := metadataValues.Get("authorization")
		if len(authorization) != 1 || !strings.HasPrefix(authorization[0], "Bearer ") {
			return nil, status.Error(codes.Unauthenticated, "bearer access token is required")
		}
		claims, err := verifier.Verify(strings.TrimPrefix(authorization[0], "Bearer "), clock.Now())
		if err != nil {
			observability.LogError(ctx, logger, "auth.verify_access_token", err, "grpc_method", info.FullMethod)
			return nil, status.Error(codes.Unauthenticated, "invalid or expired access token")
		}
		principal := Principal{UserID: claims.UserID, Roles: append([]string(nil), claims.Roles...)}
		return handler(context.WithValue(ctx, principalKey{}, principal), request)
	}
}

func AuthorizationInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !requiresAdminIdentity(info.FullMethod) {
			return handler(ctx, request)
		}
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		if !hasAdminRole(principal.Roles) {
			return nil, status.Error(codes.PermissionDenied, "administrator role is required")
		}
		return handler(ctx, request)
	}
}

func requiresAdminIdentity(method string) bool {
	return strings.HasPrefix(method, adminOrderMethodPrefix) || strings.HasPrefix(method, adminCatalogMethodPrefix)
}

func principalFromContext(ctx context.Context) (Principal, error) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	if !ok || principal.UserID == "" {
		return Principal{}, status.Error(codes.Unauthenticated, "administrator identity is missing")
	}
	return principal, nil
}

func hasAdminRole(roles []string) bool {
	for _, role := range roles {
		if role == "manager" || role == "admin" {
			return true
		}
	}
	return false
}

package grpcapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/netip"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/SV1Stail/online_market/internal/observability"
	"github.com/SV1Stail/online_market/internal/ratelimit"
)

type rateLimiter interface {
	Allow(context.Context, string, ratelimit.Policy) (*ratelimit.Decision, error)
}

type clientIPResolver interface {
	Resolve(context.Context) (netip.Addr, error)
}

func RateLimitInterceptor(
	limiter rateLimiter,
	resolver clientIPResolver,
	policies map[string]ratelimit.Policy,
	logger *slog.Logger,
) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		policy, exists := policies[info.FullMethod]
		if !exists {
			return handler(ctx, request)
		}
		clientIP, err := resolver.Resolve(ctx)
		if err != nil {
			observability.LogError(ctx, logger, "rate_limit.resolve_client_ip", err, "grpc_method", info.FullMethod)
			return nil, status.Error(codes.Internal, "internal server error")
		}
		decision, err := limiter.Allow(ctx, hashedIP(clientIP), policy)
		if err != nil {
			observability.LogError(ctx, logger, "rate_limit.allow", err, "grpc_method", info.FullMethod)
			return nil, status.Error(codes.Unavailable, "rate limiter is unavailable")
		}
		if decision == nil {
			err = errors.New("rate limiter returned an empty decision")
			observability.LogError(ctx, logger, "rate_limit.allow", err, "grpc_method", info.FullMethod)
			return nil, status.Error(codes.Unavailable, "rate limiter is unavailable")
		}
		if decision.Allowed {
			return handler(ctx, request)
		}
		return nil, rateLimitError(decision.RetryAfter, logger, ctx, info.FullMethod)
	}
}

func hashedIP(address netip.Addr) string {
	value := sha256.Sum256([]byte(normalizeIP(address).String()))
	return hex.EncodeToString(value[:])
}

func rateLimitError(retryAfter time.Duration, logger *slog.Logger, ctx context.Context, method string) error {
	if retryAfter <= 0 {
		retryAfter = time.Second
	}
	baseStatus := status.New(codes.ResourceExhausted, "rate limit exceeded")
	detailedStatus, err := baseStatus.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(retryAfter)})
	if err != nil {
		observability.LogError(ctx, logger, "rate_limit.add_retry_info", err, "grpc_method", method)
		return baseStatus.Err()
	}
	return detailedStatus.Err()
}

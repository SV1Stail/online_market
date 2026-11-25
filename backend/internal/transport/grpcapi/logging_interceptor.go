package grpcapi

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/SV1Stail/online_market/internal/observability"
)

func StructuredLoggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		startedAt := time.Now()
		response, err := handler(ctx, request)
		attributes := []any{
			"grpc_method", info.FullMethod,
			"request_id", observability.RequestID(ctx),
			"grpc_code", status.Code(err).String(),
			"duration_ms", time.Since(startedAt).Milliseconds(),
		}
		if err != nil {
			logger.ErrorContext(ctx, "gRPC call completed", attributes...)
		} else {
			logger.InfoContext(ctx, "gRPC call completed", attributes...)
		}
		return response, err
	}
}

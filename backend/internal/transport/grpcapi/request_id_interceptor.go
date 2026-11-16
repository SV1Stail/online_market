package grpcapi

import (
	"context"
	"log/slog"
	"regexp"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/SV1Stail/online_market/internal/identifier"
	"github.com/SV1Stail/online_market/internal/observability"
)

const requestIDMetadataKey = "x-request-id"

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func RequestIDInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		requestID := incomingRequestID(ctx)
		if requestID == "" {
			generated, err := identifier.NewUUID()
			if err != nil {
				observability.LogError(ctx, logger, "grpc.generate_request_id", err, "grpc_method", info.FullMethod)
				return nil, status.Error(codes.Internal, "internal server error")
			}
			requestID = generated
		}
		ctx = observability.WithRequestID(ctx, requestID)
		if err := grpc.SetHeader(ctx, metadata.Pairs(requestIDMetadataKey, requestID)); err != nil {
			observability.LogError(ctx, logger, "grpc.set_request_id_header", err, "grpc_method", info.FullMethod)
		}
		return handler(ctx, request)
	}
}

func incomingRequestID(ctx context.Context) string {
	values, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	requestIDs := values.Get(requestIDMetadataKey)
	if len(requestIDs) != 1 || !requestIDPattern.MatchString(requestIDs[0]) {
		return ""
	}
	return requestIDs[0]
}

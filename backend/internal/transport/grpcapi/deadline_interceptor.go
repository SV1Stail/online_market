package grpcapi

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

func DeadlineInterceptor(timeout time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if deadline, exists := ctx.Deadline(); exists && time.Until(deadline) <= timeout {
			return handler(ctx, request)
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return handler(ctx, request)
	}
}

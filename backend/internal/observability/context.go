package observability

import (
	"context"
	"log/slog"
)

type requestIDKey struct{}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

func RequestID(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}

func LogError(ctx context.Context, logger *slog.Logger, operation string, err error, attributes ...any) {
	if err == nil {
		return
	}
	values := []any{
		"operation", operation,
		"request_id", RequestID(ctx),
		"error", err,
	}
	values = append(values, attributes...)
	logger.ErrorContext(ctx, "operation failed", values...)
}

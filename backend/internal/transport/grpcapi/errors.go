package grpcapi

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/observability"
	"github.com/SV1Stail/online_market/internal/order"
)

func ErrorMappingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		response, err := handler(ctx, request)
		if err == nil {
			return response, nil
		}
		observability.LogError(ctx, logger, "grpc.handle", err, "grpc_method", info.FullMethod)
		return nil, toStatus(err)
	}
}

func toStatus(err error) error {
	if err == nil {
		return nil
	}
	var validation *apperror.ValidationError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request cancelled")
	case status.Code(err) != codes.Unknown:
		return err
	case errors.As(err, &validation):
		return status.Error(codes.InvalidArgument, validation.Error())
	case errors.Is(err, apperror.ErrNotFound):
		return status.Error(codes.NotFound, "resource not found")
	case errors.Is(err, apperror.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "authentication failed")
	case errors.Is(err, apperror.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, "permission denied")
	case errors.Is(err, apperror.ErrConflict):
		return status.Error(codes.Aborted, "resource was changed concurrently")
	case errors.Is(err, apperror.ErrPrecondition):
		if strings.Contains(err.Error(), order.DeliveryDayFullMessage) {
			return status.Error(codes.FailedPrecondition, order.DeliveryDayFullMessage)
		}
		if strings.Contains(err.Error(), order.OrderMinimumMessage) {
			return status.Error(codes.FailedPrecondition, order.OrderMinimumMessage)
		}
		return status.Error(codes.FailedPrecondition, "operation is not allowed in the current state")
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}

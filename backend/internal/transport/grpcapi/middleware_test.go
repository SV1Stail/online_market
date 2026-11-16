package grpcapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/observability"
)

func TestRequestIDInterceptorUsesIncomingRequestID(t *testing.T) {
	logger := discardLogger()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(requestIDMetadataKey, "request-123"))
	stream := &serverTransportStreamStub{}
	ctx = grpc.NewContextWithServerTransportStream(ctx, stream)
	interceptor := RequestIDInterceptor(logger)

	_, err := interceptor(ctx, nil, unaryServerInfo(), func(ctx context.Context, _ any) (any, error) {
		require.Equal(t, "request-123", observability.RequestID(ctx))
		return nil, nil
	})

	require.NoError(t, err)
	require.Equal(t, []string{"request-123"}, stream.header.Get(requestIDMetadataKey))
}

func TestRequestIDInterceptorReplacesInvalidRequestID(t *testing.T) {
	logger := discardLogger()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(requestIDMetadataKey, "invalid request id"))
	ctx = grpc.NewContextWithServerTransportStream(ctx, &serverTransportStreamStub{})
	interceptor := RequestIDInterceptor(logger)

	_, err := interceptor(ctx, nil, unaryServerInfo(), func(ctx context.Context, _ any) (any, error) {
		requestID := observability.RequestID(ctx)
		require.NotEmpty(t, requestID)
		require.True(t, requestIDPattern.MatchString(requestID))
		return nil, nil
	})

	require.NoError(t, err)
}

func TestDeadlineInterceptorAddsDeadline(t *testing.T) {
	const timeout = 5 * time.Second
	interceptor := DeadlineInterceptor(timeout)
	startedAt := time.Now()

	_, err := interceptor(context.Background(), nil, unaryServerInfo(), func(ctx context.Context, _ any) (any, error) {
		deadline, exists := ctx.Deadline()
		require.True(t, exists)
		require.WithinDuration(t, startedAt.Add(timeout), deadline, 250*time.Millisecond)
		return nil, nil
	})

	require.NoError(t, err)
}

func TestDeadlineInterceptorPreservesShorterDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	want, exists := ctx.Deadline()
	require.True(t, exists)

	_, err := DeadlineInterceptor(5*time.Second)(ctx, nil, unaryServerInfo(), func(ctx context.Context, _ any) (any, error) {
		got, exists := ctx.Deadline()
		require.True(t, exists)
		require.Equal(t, want, got)
		return nil, nil
	})

	require.NoError(t, err)
}

func TestRecoveryInterceptorConvertsPanicToInternalError(t *testing.T) {
	interceptor := RecoveryInterceptor(discardLogger())

	response, err := interceptor(context.Background(), nil, unaryServerInfo(), func(context.Context, any) (any, error) {
		panic("unexpected failure")
	})

	require.Nil(t, response)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestErrorMappingInterceptorMapsApplicationError(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{name: "validation", err: apperror.Validation("name", "is required"), wantCode: codes.InvalidArgument},
		{name: "not found", err: errors.Join(errors.New("load product"), apperror.ErrNotFound), wantCode: codes.NotFound},
		{name: "unauthenticated", err: apperror.ErrUnauthenticated, wantCode: codes.Unauthenticated},
		{name: "permission denied", err: apperror.ErrPermissionDenied, wantCode: codes.PermissionDenied},
		{name: "conflict", err: apperror.ErrConflict, wantCode: codes.Aborted},
		{name: "precondition", err: apperror.ErrPrecondition, wantCode: codes.FailedPrecondition},
		{name: "deadline", err: context.DeadlineExceeded, wantCode: codes.DeadlineExceeded},
		{name: "cancelled", err: context.Canceled, wantCode: codes.Canceled},
		{name: "unknown", err: errors.New("database unavailable"), wantCode: codes.Internal},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			interceptor := ErrorMappingInterceptor(discardLogger())
			_, err := interceptor(context.Background(), nil, unaryServerInfo(), func(context.Context, any) (any, error) {
				return nil, testCase.err
			})

			require.Equal(t, testCase.wantCode, status.Code(err))
		})
	}
}

func TestStructuredLoggingInterceptorWritesCallFields(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	ctx := observability.WithRequestID(context.Background(), "request-123")
	interceptor := StructuredLoggingInterceptor(logger)

	_, err := interceptor(ctx, nil, unaryServerInfo(), func(context.Context, any) (any, error) {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	})

	require.Error(t, err)
	require.Contains(t, output.String(), `"grpc_method":"/online_market.api.v1.Test/Call"`)
	require.Contains(t, output.String(), `"request_id":"request-123"`)
	require.Contains(t, output.String(), `"grpc_code":"InvalidArgument"`)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func unaryServerInfo() *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: "/online_market.api.v1.Test/Call"}
}

type serverTransportStreamStub struct {
	header metadata.MD
}

func (s *serverTransportStreamStub) Method() string {
	return unaryServerInfo().FullMethod
}

func (s *serverTransportStreamStub) SetHeader(header metadata.MD) error {
	s.header = metadata.Join(s.header, header)
	return nil
}

func (s *serverTransportStreamStub) SendHeader(header metadata.MD) error {
	return s.SetHeader(header)
}

func (s *serverTransportStreamStub) SetTrailer(metadata.MD) error {
	return nil
}

package grpcapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/SV1Stail/online_market/internal/security"
)

func TestAuthenticationInterceptorAddsPrincipalWithoutCheckingRole(t *testing.T) {
	interceptor := AuthenticationInterceptor(
		tokenVerifierStub{claims: &security.Claims{UserID: "admin-1", Roles: []string{"viewer"}}},
		timeSourceStub{},
		discardLogger(),
	)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer token"))
	handlerCalled := false

	_, err := interceptor(ctx, nil, adminUnaryServerInfo(), func(ctx context.Context, _ any) (any, error) {
		handlerCalled = true
		principal, principalErr := principalFromContext(ctx)
		require.NoError(t, principalErr)
		require.Equal(t, "admin-1", principal.UserID)
		require.Equal(t, []string{"viewer"}, principal.Roles)
		return nil, nil
	})

	require.NoError(t, err)
	require.True(t, handlerCalled)
}

func TestAuthenticationInterceptorRejectsInvalidToken(t *testing.T) {
	interceptor := AuthenticationInterceptor(
		tokenVerifierStub{err: errors.New("invalid token")},
		timeSourceStub{},
		discardLogger(),
	)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer token"))

	_, err := interceptor(ctx, nil, adminUnaryServerInfo(), func(context.Context, any) (any, error) {
		return nil, nil
	})

	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthorizationInterceptorChecksRole(t *testing.T) {
	testCases := []struct {
		name     string
		roles    []string
		wantCode codes.Code
	}{
		{name: "admin", roles: []string{"admin"}, wantCode: codes.OK},
		{name: "manager", roles: []string{"manager"}, wantCode: codes.OK},
		{name: "another role", roles: []string{"viewer"}, wantCode: codes.PermissionDenied},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), principalKey{}, Principal{UserID: "admin-1", Roles: testCase.roles})
			_, err := AuthorizationInterceptor()(ctx, nil, adminUnaryServerInfo(), func(context.Context, any) (any, error) {
				return nil, nil
			})

			require.Equal(t, testCase.wantCode, status.Code(err))
		})
	}
}

func TestAuthorizationInterceptorSkipsPublicMethod(t *testing.T) {
	_, err := AuthorizationInterceptor()(context.Background(), nil, unaryServerInfo(), func(context.Context, any) (any, error) {
		return nil, nil
	})

	require.NoError(t, err)
}

func TestAdminCatalogRequiresAuthentication(t *testing.T) {
	require.True(t, requiresAdminIdentity("/online_market.api.v1.AdminCatalogService/CreateProduct"))
}

type tokenVerifierStub struct {
	claims *security.Claims
	err    error
}

func (s tokenVerifierStub) Verify(string, time.Time) (*security.Claims, error) {
	return s.claims, s.err
}

type timeSourceStub struct{}

func (timeSourceStub) Now() time.Time {
	return time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC)
}

func adminUnaryServerInfo() *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: "/online_market.api.v1.AdminOrderService/GetOrder"}
}

package grpcapi

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/SV1Stail/online_market/internal/ratelimit"
)

func TestRateLimitInterceptorAllowsRequest(t *testing.T) {
	limiter := &rateLimiterStub{decision: &ratelimit.Decision{Allowed: true}}
	policy := ratelimit.Policy{Name: "catalog", Rules: []ratelimit.Rule{{Limit: 100, Window: time.Minute}}}
	interceptor := RateLimitInterceptor(
		limiter,
		clientIPResolverStub{address: netip.MustParseAddr("203.0.113.10")},
		map[string]ratelimit.Policy{unaryServerInfo().FullMethod: policy},
		discardLogger(),
	)
	handlerCalled := false

	_, err := interceptor(context.Background(), nil, unaryServerInfo(), func(context.Context, any) (any, error) {
		handlerCalled = true
		return nil, nil
	})

	require.NoError(t, err)
	require.True(t, handlerCalled)
	require.Equal(t, policy, limiter.policy)
	require.Len(t, limiter.subject, 64)
}

func TestRateLimitInterceptorRejectsWithRetryInfo(t *testing.T) {
	interceptor := RateLimitInterceptor(
		&rateLimiterStub{decision: &ratelimit.Decision{RetryAfter: 3 * time.Second}},
		clientIPResolverStub{address: netip.MustParseAddr("203.0.113.10")},
		map[string]ratelimit.Policy{unaryServerInfo().FullMethod: {Name: "catalog", Rules: []ratelimit.Rule{{Limit: 1, Window: time.Minute}}}},
		discardLogger(),
	)

	_, err := interceptor(context.Background(), nil, unaryServerInfo(), func(context.Context, any) (any, error) {
		return nil, nil
	})

	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	statusValue, ok := status.FromError(err)
	require.True(t, ok)
	require.Len(t, statusValue.Details(), 1)
	retryInfo, ok := statusValue.Details()[0].(*errdetails.RetryInfo)
	require.True(t, ok)
	require.Equal(t, 3*time.Second, retryInfo.GetRetryDelay().AsDuration())
}

func TestRateLimitInterceptorReturnsUnavailableOnRedisError(t *testing.T) {
	interceptor := RateLimitInterceptor(
		&rateLimiterStub{err: errors.New("redis unavailable")},
		clientIPResolverStub{address: netip.MustParseAddr("203.0.113.10")},
		map[string]ratelimit.Policy{unaryServerInfo().FullMethod: {Name: "catalog", Rules: []ratelimit.Rule{{Limit: 1, Window: time.Minute}}}},
		discardLogger(),
	)

	_, err := interceptor(context.Background(), nil, unaryServerInfo(), func(context.Context, any) (any, error) {
		return nil, nil
	})

	require.Equal(t, codes.Unavailable, status.Code(err))
}

type rateLimiterStub struct {
	decision *ratelimit.Decision
	err      error
	subject  string
	policy   ratelimit.Policy
}

func (s *rateLimiterStub) Allow(_ context.Context, subject string, policy ratelimit.Policy) (*ratelimit.Decision, error) {
	s.subject = subject
	s.policy = policy
	return s.decision, s.err
}

type clientIPResolverStub struct {
	address netip.Addr
	err     error
}

func (s clientIPResolverStub) Resolve(context.Context) (netip.Addr, error) {
	return s.address, s.err
}

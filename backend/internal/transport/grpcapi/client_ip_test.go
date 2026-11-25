package grpcapi

import (
	"context"
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

func TestClientIPResolverUsesDirectPeer(t *testing.T) {
	ctx := peerContext("203.0.113.10")

	address, err := NewClientIPResolver(nil).Resolve(ctx)

	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("203.0.113.10"), address)
}

func TestClientIPResolverIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	ctx := peerContext("203.0.113.10")
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-forwarded-for", "198.51.100.20"))

	address, err := NewClientIPResolver(nil).Resolve(ctx)

	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("203.0.113.10"), address)
}

func TestClientIPResolverUsesForwardedHeaderFromTrustedProxy(t *testing.T) {
	ctx := peerContext("10.0.0.2")
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		"x-forwarded-for",
		"198.51.100.20, 10.0.0.3",
	))
	resolver := NewClientIPResolver([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})

	address, err := resolver.Resolve(ctx)

	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("198.51.100.20"), address)
}

func peerContext(address string) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		Addr: &net.TCPAddr{IP: net.ParseIP(address), Port: 443},
	})
}

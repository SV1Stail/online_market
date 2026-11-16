package grpcapi

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

type ClientIPResolver struct {
	trustedProxies []netip.Prefix
}

func NewClientIPResolver(trustedProxies []netip.Prefix) *ClientIPResolver {
	return &ClientIPResolver{trustedProxies: append([]netip.Prefix(nil), trustedProxies...)}
}

func (r *ClientIPResolver) Resolve(ctx context.Context) (netip.Addr, error) {
	peerValue, exists := peer.FromContext(ctx)
	if !exists || peerValue.Addr == nil {
		return netip.Addr{}, fmt.Errorf("gRPC peer address is missing")
	}
	peerIP, err := addressIP(peerValue.Addr)
	if err != nil {
		return netip.Addr{}, err
	}
	if !r.isTrusted(peerIP) {
		return peerIP, nil
	}

	metadataValues, exists := metadata.FromIncomingContext(ctx)
	if !exists {
		return peerIP, nil
	}
	forwarded := forwardedAddresses(metadataValues.Get("x-forwarded-for"))
	current := peerIP
	for index := len(forwarded) - 1; index >= 0 && r.isTrusted(current); index-- {
		candidate, err := netip.ParseAddr(forwarded[index])
		if err != nil {
			return peerIP, nil
		}
		current = normalizeIP(candidate)
	}
	return current, nil
}

func (r *ClientIPResolver) isTrusted(address netip.Addr) bool {
	for _, prefix := range r.trustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func addressIP(address net.Addr) (netip.Addr, error) {
	if tcpAddress, ok := address.(*net.TCPAddr); ok {
		value, exists := netip.AddrFromSlice(tcpAddress.IP)
		if !exists {
			return netip.Addr{}, fmt.Errorf("gRPC peer IP is invalid")
		}
		return normalizeIP(value), nil
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse gRPC peer address: %w", err)
	}
	value, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse gRPC peer IP: %w", err)
	}
	return normalizeIP(value), nil
}

func normalizeIP(address netip.Addr) netip.Addr {
	return address.Unmap().WithZone("")
}

func forwardedAddresses(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		}
	}
	return result
}

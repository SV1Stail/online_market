package config

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrefixesParsesMasksAndRemovesDuplicates(t *testing.T) {
	result, err := prefixes("TRUSTED_PROXY_CIDRS", "10.0.0.2/8, 10.0.0.0/8, 2001:db8::/32")

	require.NoError(t, err)
	require.Equal(t, []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("2001:db8::/32"),
	}, result)
}

func TestPrefixesRejectsInvalidCIDR(t *testing.T) {
	_, err := prefixes("TRUSTED_PROXY_CIDRS", "10.0.0.1")

	require.EqualError(t, err, `TRUSTED_PROXY_CIDRS contains invalid CIDR "10.0.0.1"`)
}

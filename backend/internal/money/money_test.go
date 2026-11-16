package money

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMoneyMultiply(t *testing.T) {
	price, err := New(1250, "RUB")
	require.NoError(t, err)

	total, err := price.Multiply(3)
	require.NoError(t, err)
	require.Equal(t, Money{MinorUnits: 3750, Currency: "RUB"}, total)
}

func TestMoneyRejectsCurrencyMismatch(t *testing.T) {
	rubles, err := New(100, "RUB")
	require.NoError(t, err)
	dollars, err := New(100, "USD")
	require.NoError(t, err)

	_, err = rubles.Add(dollars)
	require.Error(t, err)
}

func TestMoneyDetectsOverflow(t *testing.T) {
	price, err := New(math.MaxInt64, "RUB")
	require.NoError(t, err)

	_, err = price.Multiply(2)
	require.Error(t, err)
}

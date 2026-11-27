package order

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/money"
)

func TestCheckOrderMinimum(t *testing.T) {
	enough, err := money.New(300_000, "RUB")
	require.NoError(t, err)
	short, err := money.New(299_999, "RUB")
	require.NoError(t, err)

	require.NoError(t, CheckOrderMinimum(short, 0))
	require.NoError(t, CheckOrderMinimum(enough, 300_000))
	err = CheckOrderMinimum(short, 300_000)
	require.ErrorIs(t, err, apperror.ErrPrecondition)
	require.ErrorContains(t, err, OrderMinimumMessage)
}

func TestCheckLineMinimum(t *testing.T) {
	line, err := money.New(2_000_00, "RUB")
	require.NoError(t, err)
	minimum, err := money.New(2_000_00, "RUB")
	require.NoError(t, err)
	higher, err := money.New(3_000_00, "RUB")
	require.NoError(t, err)
	zero, err := money.New(0, "RUB")
	require.NoError(t, err)

	require.NoError(t, CheckLineMinimum("Хлеб", 1, line, 0, nil))
	require.NoError(t, CheckLineMinimum("Хлеб", 1, line, 0, &zero))
	require.NoError(t, CheckLineMinimum("Картофель", 10, line, 10, nil))
	require.NoError(t, CheckLineMinimum("Картофель", 2, line, 0, &minimum))
	require.NoError(t, CheckLineMinimum("Картофель", 10, line, 10, &minimum))

	quantityErr := CheckLineMinimum("Картофель", 9, line, 10, nil)
	require.ErrorIs(t, quantityErr, apperror.ErrPrecondition)
	require.ErrorContains(t, quantityErr, "Картофель")
	require.ErrorContains(t, quantityErr, "minimum quantity")

	amountErr := CheckLineMinimum("Картофель", 2, line, 0, &higher)
	require.ErrorIs(t, amountErr, apperror.ErrPrecondition)
	require.ErrorContains(t, amountErr, "minimum amount")

	bothErr := CheckLineMinimum("Картофель", 1, line, 10, &higher)
	require.ErrorIs(t, bothErr, apperror.ErrPrecondition)
	require.ErrorContains(t, bothErr, "minimum quantity")
}

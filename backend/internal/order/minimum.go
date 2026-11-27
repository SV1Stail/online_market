package order

import (
	"fmt"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/money"
)

const OrderMinimumMessage = "order total is below the minimum"

// CheckOrderMinimum reports whether the whole order reaches the shop minimum.
// A non-positive minimum imposes no limit.
func CheckOrderMinimum(total money.Money, minimumMinor int64) error {
	if minimumMinor <= 0 {
		return nil
	}
	if total.Currency != "RUB" || total.MinorUnits < minimumMinor {
		return fmt.Errorf("%w: %s", apperror.ErrPrecondition, OrderMinimumMessage)
	}
	return nil
}

// CheckLineMinimum reports whether one order line meets the product minimums.
// A zero minQuantity and a nil or non-positive minAmount impose no limit.
// When both limits are set, the line must satisfy both.
func CheckLineMinimum(productName string, quantity uint32, lineTotal money.Money, minQuantity uint32, minAmount *money.Money) error {
	if minQuantity > 0 && quantity < minQuantity {
		return fmt.Errorf("%w: product %s does not meet the minimum quantity of %d", apperror.ErrPrecondition, productName, minQuantity)
	}
	if minAmount == nil || minAmount.MinorUnits < 1 {
		return nil
	}
	if lineTotal.Currency != minAmount.Currency || lineTotal.MinorUnits < minAmount.MinorUnits {
		return fmt.Errorf("%w: product %s does not meet the minimum amount of %d %s", apperror.ErrPrecondition, productName, minAmount.MinorUnits, minAmount.Currency)
	}
	return nil
}

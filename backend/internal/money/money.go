package money

import (
	"fmt"
	"regexp"
)

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

type Money struct {
	MinorUnits int64
	Currency   string
}

func New(minorUnits int64, currency string) (Money, error) {
	if minorUnits < 0 {
		return Money{}, fmt.Errorf("amount must not be negative")
	}
	if !currencyPattern.MatchString(currency) {
		return Money{}, fmt.Errorf("currency must be an uppercase ISO 4217 code")
	}
	return Money{MinorUnits: minorUnits, Currency: currency}, nil
}

func (m Money) Multiply(quantity uint32) (Money, error) {
	if quantity == 0 {
		return Money{}, fmt.Errorf("quantity must be positive")
	}
	if m.MinorUnits > 0 && int64(quantity) > (1<<63-1)/m.MinorUnits {
		return Money{}, fmt.Errorf("amount overflow")
	}
	return Money{MinorUnits: m.MinorUnits * int64(quantity), Currency: m.Currency}, nil
}

func (m Money) Add(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("currency mismatch")
	}
	if other.MinorUnits > 0 && m.MinorUnits > (1<<63-1)-other.MinorUnits {
		return Money{}, fmt.Errorf("amount overflow")
	}
	return Money{MinorUnits: m.MinorUnits + other.MinorUnits, Currency: m.Currency}, nil
}

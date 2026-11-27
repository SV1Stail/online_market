package order

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePhoneAcceptsMobileVariants(t *testing.T) {
	customer := Customer{FullName: "Покупатель"}
	for _, phone := range []string{
		"+71234567890",
		"81234567890",
		"+7(123)4567890",
		"7-123-456-7890",
		"+7 (123) 456-78-90",
	} {
		customer.Phone = phone
		require.NoError(t, validateCustomer(customer), phone)
	}
}

func TestValidatePhoneRejectsOtherCharacters(t *testing.T) {
	customer := Customer{FullName: "Покупатель"}
	for _, phone := range []string{"abc", "8123abc", "8 123 45 67!", "()", "+", " "} {
		customer.Phone = phone
		require.Error(t, validateCustomer(customer), phone)
	}
}

package order

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanTransition(t *testing.T) {
	tests := []struct {
		name string
		from Status
		to   Status
		want bool
	}{
		{name: "new order can be confirmed", from: StatusNew, to: StatusConfirmed, want: true},
		{name: "new order can be cancelled", from: StatusNew, to: StatusCancelled, want: true},
		{name: "ready pickup can complete", from: StatusReady, to: StatusCompleted, want: true},
		{name: "delivery can complete", from: StatusInDelivery, to: StatusCompleted, want: true},
		{name: "completed order is terminal", from: StatusCompleted, to: StatusCancelled, want: false},
		{name: "cancelled order is terminal", from: StatusCancelled, to: StatusNew, want: false},
		{name: "status cannot transition to itself", from: StatusNew, to: StatusNew, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, CanTransition(test.from, test.to))
		})
	}
}

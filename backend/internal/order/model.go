package order

import (
	"fmt"
	"time"

	"github.com/SV1Stail/online_market/internal/money"
)

type Status string

const (
	StatusNew        Status = "new"
	StatusConfirmed  Status = "confirmed"
	StatusAssembling Status = "assembling"
	StatusReady      Status = "ready"
	StatusInDelivery Status = "in_delivery"
	StatusCompleted  Status = "completed"
	StatusCancelled  Status = "cancelled"
)

type DeliveryMethod string

const (
	DeliveryPickup  DeliveryMethod = "pickup"
	DeliveryCourier DeliveryMethod = "courier"
)

type Customer struct {
	FullName string
	Phone    string
	Email    string
}

type Delivery struct {
	Method  DeliveryMethod
	City    string
	Address string
	Comment string
	Date    time.Time
}

type Item struct {
	ID            string
	ProductID     string
	ProductName   string
	UnitPrice     money.Money
	Quantity      uint32
	LineTotal     money.Money
	UnitOfMeasure string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Order struct {
	ID              string
	Number          string
	Name            string
	CartID          string
	Customer        Customer
	Delivery        Delivery
	CustomerComment string
	InternalComment string
	AssignedAdminID string
	Status          Status
	Items           []Item
	Total           money.Money
	Version         uint64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type EventType string

const (
	EventCreated        EventType = "created"
	EventDetailsChanged EventType = "details_changed"
	EventItemAdded      EventType = "item_added"
	EventItemChanged    EventType = "item_changed"
	EventItemRemoved    EventType = "item_removed"
	EventStatusChanged  EventType = "status_changed"
)

type FieldChange struct {
	FieldPath string
	OldValue  string
	NewValue  string
}

type Event struct {
	ID           string
	OrderID      string
	Type         EventType
	ActorAdminID string
	Comment      string
	Changes      []FieldChange
	CreatedAt    time.Time
}

func CanTransition(from, to Status) bool {
	if from == to {
		return false
	}
	allowed := map[Status]map[Status]struct{}{
		StatusNew: {
			StatusConfirmed: {},
			StatusCancelled: {},
		},
		StatusConfirmed: {
			StatusAssembling: {},
			StatusCancelled:  {},
		},
		StatusAssembling: {
			StatusReady:     {},
			StatusCancelled: {},
		},
		StatusReady: {
			StatusInDelivery: {},
			StatusCompleted:  {},
			StatusCancelled:  {},
		},
		StatusInDelivery: {
			StatusCompleted: {},
		},
	}
	_, ok := allowed[from][to]
	return ok
}

func ParseStatus(value string) (Status, error) {
	status := Status(value)
	switch status {
	case StatusNew, StatusConfirmed, StatusAssembling, StatusReady, StatusInDelivery, StatusCompleted, StatusCancelled:
		return status, nil
	default:
		return "", fmt.Errorf("unknown order status %q", value)
	}
}

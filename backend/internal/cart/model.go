package cart

import (
	"time"

	"github.com/SV1Stail/online_market/internal/catalog"
	"github.com/SV1Stail/online_market/internal/money"
)

type Status string

const (
	StatusOpen       Status = "open"
	StatusCheckedOut Status = "checked_out"
	StatusExpired    Status = "expired"
)

type Item struct {
	ID           string
	ProductID    string
	ProductName  string
	UnitPrice    money.Money
	Quantity     uint32
	LineTotal    money.Money
	Availability catalog.Availability
}

type Cart struct {
	ID        string
	Status    Status
	Items     []Item
	Total     money.Money
	Version   uint64
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time
}

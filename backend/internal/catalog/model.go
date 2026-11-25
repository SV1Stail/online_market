package catalog

import (
	"time"

	"github.com/SV1Stail/online_market/internal/money"
	"github.com/SV1Stail/online_market/internal/pagination"
)

type Availability string

const (
	AvailabilityAvailable  Availability = "available"
	AvailabilityOutOfStock Availability = "out_of_stock"
	AvailabilityArchived   Availability = "archived"
)

type Product struct {
	ID           string
	Name         string
	Description  string
	Price        money.Money
	Availability Availability
	CategoryIDs  []string
	Version      uint64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Category struct {
	ID        string
	Name      string
	ParentID  string
	SortOrder uint32
	Version   uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ListFilter struct {
	CategoryIDs   []string
	Query         string
	OnlyAvailable bool
	Cursor        *pagination.Cursor
	Limit         int
}

type ListResult struct {
	Products      []Product
	NextPageToken string
}

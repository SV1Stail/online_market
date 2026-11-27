package order

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/SV1Stail/online_market/internal/apperror"
)

const (
	DeliveryDateLayout             = "2006-01-02"
	DeliveryHorizonDays            = 60
	MaxDeliveryDayLimit            = 10_000
	DeliveryDayFullMessage         = "delivery day is fully booked"
	DeliveryCityUnavailableMessage = "delivery city is not available"
	DeliveryDateUnavailableMessage = "delivery date is not available for the city"
	ScheduleAll                    = "all"
	ScheduleWeekdays               = "weekdays"
	ScheduleDates                  = "dates"
)

// ShopLocation is the calendar used for delivery days. The store is in Yakutsk.
var ShopLocation = mustShopLocation()

type DeliveryDay struct {
	Date   time.Time
	Booked int
	Limit  int
}

func (d DeliveryDay) Remaining() int {
	if d.Booked >= d.Limit {
		return 0
	}
	return d.Limit - d.Booked
}

func ShopToday(now time.Time) time.Time {
	year, month, day := now.In(ShopLocation).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func FormatDeliveryDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(DeliveryDateLayout)
}

func ParseDeliveryDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(DeliveryDateLayout, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}

func SameDeliveryDate(left, right time.Time) bool {
	return FormatDeliveryDate(left) == FormatDeliveryDate(right)
}

func validateCustomerDeliveryDate(date, now time.Time) error {
	if date.IsZero() {
		return apperror.Validation("delivery.date", "is required")
	}
	today := ShopToday(now)
	last := today.AddDate(0, 0, DeliveryHorizonDays-1)
	if date.Before(today) || date.After(last) {
		return apperror.Validation("delivery.date", "must be within the delivery window")
	}
	return nil
}

func validateAdminDeliveryDate(date time.Time) error {
	if date.IsZero() {
		return apperror.Validation("delivery.date", "is required")
	}
	return nil
}

func validateDeliveryLimit(limit int) error {
	if limit < 0 || limit > MaxDeliveryDayLimit {
		return apperror.Validation("delivery_limit", "must be between 0 and 10000")
	}
	return nil
}

func deliveryWindow(now time.Time) (time.Time, time.Time) {
	today := ShopToday(now)
	return today, today.AddDate(0, 0, DeliveryHorizonDays-1)
}

func normalizeDeliveryList(from time.Time, days int, now time.Time) (time.Time, int) {
	today, last := deliveryWindow(now)
	if from.IsZero() || from.Before(today) {
		from = today
	}
	if from.After(last) {
		return from, 0
	}
	if days <= 0 || days > DeliveryHorizonDays {
		days = DeliveryHorizonDays
	}
	if end := from.AddDate(0, 0, days-1); end.After(last) {
		days = int(last.Sub(from).Hours()/24) + 1
	}
	return from, days
}

func deliveryDayFullError() error {
	return fmt.Errorf("%w: %s", apperror.ErrPrecondition, DeliveryDayFullMessage)
}

type DeliveryPlace struct {
	ID           string
	Name         string
	DailyLimit   int
	ScheduleKind string
	Dates        []time.Time
}

func PlaceKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func IsWeekday(date time.Time) bool {
	weekday := date.UTC().Weekday()
	return weekday >= time.Monday && weekday <= time.Friday
}

func PlaceServes(kind string, date time.Time, dates []time.Time) bool {
	switch kind {
	case ScheduleAll:
		return !date.IsZero()
	case ScheduleWeekdays:
		return !date.IsZero() && IsWeekday(date)
	case ScheduleDates:
		key := FormatDeliveryDate(date)
		for _, candidate := range dates {
			if FormatDeliveryDate(candidate) == key {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func ScheduledDates(kind string, from time.Time, days int, custom []time.Time) []time.Time {
	if days <= 0 || from.IsZero() {
		return nil
	}
	result := make([]time.Time, 0, days)
	for index := 0; index < days; index++ {
		date := from.AddDate(0, 0, index)
		if PlaceServes(kind, date, custom) {
			result = append(result, date)
		}
	}
	return result
}

func validateDeliveryPlace(place DeliveryPlace, now time.Time) error {
	name := strings.TrimSpace(place.Name)
	if name == "" || len(name) > 255 {
		return apperror.Validation("name", "must contain between 1 and 255 characters")
	}
	if err := validateDeliveryLimit(place.DailyLimit); err != nil {
		return err
	}
	switch place.ScheduleKind {
	case ScheduleAll, ScheduleWeekdays:
		return nil
	case ScheduleDates:
		if len(place.Dates) == 0 {
			return apperror.Validation("dates", "choose at least one delivery day")
		}
		if len(place.Dates) > DeliveryHorizonDays {
			return apperror.Validation("dates", "too many delivery days")
		}
		today, last := deliveryWindow(now)
		seen := make(map[string]struct{}, len(place.Dates))
		for _, date := range place.Dates {
			if date.IsZero() || date.Before(today) || date.After(last) {
				return apperror.Validation("dates", "must be within the delivery window")
			}
			key := FormatDeliveryDate(date)
			if _, exists := seen[key]; exists {
				return apperror.Validation("dates", "contains a duplicate day")
			}
			seen[key] = struct{}{}
		}
		return nil
	default:
		return apperror.Validation("schedule_kind", "is required")
	}
}

func mustShopLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Yakutsk")
	if err != nil {
		panic(err)
	}
	return location
}

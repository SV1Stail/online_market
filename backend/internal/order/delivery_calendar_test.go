package order

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SV1Stail/online_market/internal/apperror"
)

func TestCustomerDeliveryDateWindow(t *testing.T) {
	now := time.Date(2026, time.November, 25, 20, 0, 0, 0, time.UTC)
	today := ShopToday(now)
	require.Equal(t, "2026-11-26", FormatDeliveryDate(today))

	require.NoError(t, validateCustomerDeliveryDate(today, now))
	require.NoError(t, validateCustomerDeliveryDate(today.AddDate(0, 0, DeliveryHorizonDays-1), now))
	require.Error(t, validateCustomerDeliveryDate(time.Time{}, now))
	require.Error(t, validateCustomerDeliveryDate(today.AddDate(0, 0, -1), now))
	require.Error(t, validateCustomerDeliveryDate(today.AddDate(0, 0, DeliveryHorizonDays), now))
}

func TestDeliveryDayRemaining(t *testing.T) {
	require.Equal(t, 1, (DeliveryDay{Booked: 19, Limit: 20}).Remaining())
	require.Equal(t, 0, (DeliveryDay{Booked: 20, Limit: 20}).Remaining())
	require.Equal(t, 0, (DeliveryDay{Booked: 0, Limit: 0}).Remaining())
	require.ErrorIs(t, deliveryDayFullError(), apperror.ErrPrecondition)
}

func TestPlaceSchedule(t *testing.T) {
	thursday := time.Date(2026, time.November, 26, 0, 0, 0, 0, time.UTC)
	saturday := time.Date(2026, time.November, 28, 0, 0, 0, 0, time.UTC)
	require.True(t, IsWeekday(thursday))
	require.False(t, IsWeekday(saturday))
	require.True(t, PlaceServes(ScheduleAll, saturday, nil))
	require.True(t, PlaceServes(ScheduleWeekdays, thursday, nil))
	require.False(t, PlaceServes(ScheduleWeekdays, saturday, nil))
	require.True(t, PlaceServes(ScheduleDates, saturday, []time.Time{saturday}))
	require.False(t, PlaceServes(ScheduleDates, thursday, []time.Time{saturday}))

	dates := ScheduledDates(ScheduleWeekdays, thursday, 3, nil)
	require.Equal(t, []time.Time{thursday, thursday.AddDate(0, 0, 1)}, dates)
}

func TestValidateCustomPlaceAcceptsChosenDay(t *testing.T) {
	now := time.Date(2026, time.November, 26, 3, 0, 0, 0, time.UTC)
	err := validateDeliveryPlace(DeliveryPlace{
		Name:         "Лучинки",
		DailyLimit:   30,
		ScheduleKind: ScheduleDates,
		Dates:        []time.Time{time.Date(2026, time.November, 28, 0, 0, 0, 0, time.UTC)},
	}, now)
	require.NoError(t, err)
}

func TestNormalizeDeliveryListClampsToHorizon(t *testing.T) {
	now := time.Date(2026, time.November, 25, 3, 0, 0, 0, time.UTC)
	from, days := normalizeDeliveryList(time.Time{}, 0, now)
	require.Equal(t, ShopToday(now), from)
	require.Equal(t, DeliveryHorizonDays, days)

	late := ShopToday(now).AddDate(0, 0, DeliveryHorizonDays-2)
	from, days = normalizeDeliveryList(late, 10, now)
	require.Equal(t, late, from)
	require.Equal(t, 2, days)
}

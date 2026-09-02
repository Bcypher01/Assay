package instrument

import (
	"testing"

	"github.com/bcypher01/assay/internal/core"
)

func TestContinuousCalendarIsAlwaysOpen(t *testing.T) {
	c := Continuous()
	for _, ts := range []string{
		"2026-08-22T00:00:00Z", // Saturday
		"2026-08-23T12:00:00Z", // Sunday
		"2026-08-25T09:00:00Z", // Tuesday
	} {
		if !c.IsOpen(core.MustParseTime(ts)) {
			t.Fatalf("continuous calendar closed at %s", ts)
		}
	}
}

func TestFXCalendarWeekend(t *testing.T) {
	spec, err := MustDefault().Get("DUKAS:EURUSD")
	if err != nil {
		t.Fatal(err)
	}
	cal := spec.Calendar

	cases := []struct {
		name string
		ts   string
		open bool
	}{
		// New York is UTC-4 in August.
		{"Wednesday midday", "2026-08-19T12:00:00Z", true},
		{"Friday before 17:00 NY", "2026-08-21T20:00:00Z", true},
		{"Friday after 17:00 NY", "2026-08-21T22:00:00Z", false},
		{"Saturday", "2026-08-22T12:00:00Z", false},
		{"Sunday before 17:00 NY", "2026-08-23T18:00:00Z", false},
		{"Sunday after 17:00 NY", "2026-08-23T22:00:00Z", true},
		{"Christmas holiday", "2026-12-25T12:00:00Z", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cal.IsOpen(core.MustParseTime(c.ts)); got != c.open {
				t.Fatalf("IsOpen(%s) = %v, want %v", c.ts, got, c.open)
			}
		})
	}
}

func TestCalendarFollowsLocalClockAcrossDST(t *testing.T) {
	spec, _ := MustDefault().Get("DUKAS:EURUSD")
	cal := spec.Calendar

	// The Friday close is 17:00 New York, so its UTC offset shifts by an hour
	// across the daylight-saving transitions. A calendar defined in UTC would
	// be wrong for half the year.
	cases := []struct {
		name string
		ts   string
		open bool
	}{
		{"winter, 16:30 EST", "2026-01-09T21:30:00Z", true},
		{"winter, 17:30 EST", "2026-01-09T22:30:00Z", false},
		{"summer, 16:30 EDT", "2026-08-21T20:30:00Z", true},
		{"summer, 17:30 EDT", "2026-08-21T21:30:00Z", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cal.IsOpen(core.MustParseTime(c.ts)); got != c.open {
				t.Fatalf("IsOpen(%s) = %v, want %v", c.ts, got, c.open)
			}
		})
	}

	// The same wall-clock instant in UTC lands on opposite sides of the
	// boundary depending on the season, which is the point.
	if cal.IsOpen(core.MustParseTime("2026-08-21T21:30:00Z")) ==
		cal.IsOpen(core.MustParseTime("2026-01-09T21:30:00Z")) {
		t.Fatal("21:30 UTC on a Friday should differ between summer and winter")
	}
}

func TestNextOpen(t *testing.T) {
	spec, _ := MustDefault().Get("DUKAS:EURUSD")
	cal := spec.Calendar

	// From Saturday midday, the next open is Sunday 17:00 New York.
	got, ok := cal.NextOpen(core.MustParseTime("2026-08-22T12:00:00Z"))
	if !ok {
		t.Fatal("NextOpen found nothing within two weeks")
	}
	want := core.MustParseTime("2026-08-23T21:00:00Z")
	if got != want {
		t.Fatalf("NextOpen = %s, want %s", got.Std(), want.Std())
	}

	// From an instant that is already open, NextOpen returns it unchanged.
	open := core.MustParseTime("2026-08-19T12:00:00Z")
	if got, _ := cal.NextOpen(open); got != open {
		t.Fatal("NextOpen must return an already-open instant unchanged")
	}
}

func TestCalendarRejectsBadConfig(t *testing.T) {
	c := &Calendar{Location: "Mars/Olympus"}
	if err := c.init(); err == nil {
		t.Fatal("an unknown location must be rejected")
	}
	c = &Calendar{Location: "UTC", Holidays: []string{"25th December"}}
	if err := c.init(); err == nil {
		t.Fatal("a malformed holiday must be rejected")
	}
	c = &Calendar{Location: "UTC", Windows: []Window{{StartMin: 100, EndMin: 50}}}
	if err := c.init(); err == nil {
		t.Fatal("an inverted window must be rejected")
	}
}

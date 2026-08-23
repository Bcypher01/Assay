package instrument

import (
	"time"

	"github.com/Bcypher01/Assay/internal/core"
)

// Window is a trading window on a given weekday, expressed in minutes from
// midnight in the calendar's location. A window may run past midnight, in
// which case EndMin exceeds 1440.
type Window struct {
	Weekday  time.Weekday `json:"weekday"`
	StartMin int          `json:"startMin"`
	EndMin   int          `json:"endMin"`
}

// Calendar describes when an instrument trades.
//
// Sessions are expressed in a named location rather than UTC so that London
// and New York keep their local clock across daylight-saving transitions,
// which is what traders mean by those names.
type Calendar struct {
	Name     string   `json:"name"`
	Location string   `json:"location"`
	Windows  []Window `json:"windows"`
	Holidays []string `json:"holidays"` // YYYY-MM-DD in the calendar's location

	loc      *time.Location
	holidays map[string]struct{}
}

// Continuous returns a calendar that is always open, for 24/7 instruments.
func Continuous() *Calendar {
	c := &Calendar{Name: "continuous", Location: "UTC"}
	for d := time.Sunday; d <= time.Saturday; d++ {
		c.Windows = append(c.Windows, Window{Weekday: d, StartMin: 0, EndMin: 1440})
	}
	_ = c.init()
	return c
}

func (c *Calendar) init() error {
	loc := time.UTC
	if c.Location != "" && c.Location != "UTC" {
		l, err := time.LoadLocation(c.Location)
		if err != nil {
			return errField("calendar.location", err.Error())
		}
		loc = l
	}
	c.loc = loc
	c.holidays = make(map[string]struct{}, len(c.Holidays))
	for _, h := range c.Holidays {
		if _, err := time.Parse("2006-01-02", h); err != nil {
			return errField("calendar.holidays", "not a YYYY-MM-DD date: "+h)
		}
		c.holidays[h] = struct{}{}
	}
	for _, w := range c.Windows {
		if w.StartMin < 0 || w.EndMin <= w.StartMin || w.EndMin > 2880 {
			return errField("calendar.windows", "window bounds out of range")
		}
	}
	return nil
}

// IsOpen reports whether the instrument trades at t.
func (c *Calendar) IsOpen(t core.Time) bool {
	if c == nil || c.loc == nil {
		return true
	}
	lt := t.Std().In(c.loc)
	if _, ok := c.holidays[lt.Format("2006-01-02")]; ok {
		return false
	}
	min := lt.Hour()*60 + lt.Minute()

	for _, w := range c.Windows {
		// Same-day portion.
		if w.Weekday == lt.Weekday() && min >= w.StartMin && min < w.EndMin && w.EndMin <= 1440 {
			return true
		}
		// Window running past midnight, evaluated from the previous day.
		if w.EndMin > 1440 {
			if w.Weekday == lt.Weekday() && min >= w.StartMin {
				return true
			}
			prev := (int(lt.Weekday()) + 6) % 7
			if int(w.Weekday) == prev && min < w.EndMin-1440 {
				return true
			}
		}
	}
	return false
}

// NextOpen returns the first instant at or after t at which the instrument
// trades. It scans minute by minute for at most two weeks and returns
// (t, false) if it finds nothing, which indicates a malformed calendar.
func (c *Calendar) NextOpen(t core.Time) (core.Time, bool) {
	if c == nil || c.loc == nil {
		return t, true
	}
	const step = core.Time(60 * 1000)
	limit := t + core.Time(14*24*60*60*1000)
	for cur := t; cur <= limit; cur += step {
		if c.IsOpen(cur) {
			return cur, true
		}
	}
	return t, false
}

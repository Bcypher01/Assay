package core

import "time"

// Time is a timestamp in milliseconds since the Unix epoch, always UTC.
//
// Not time.Time: series hold millions of these, and an int64 keeps them
// cache-friendly and trivially comparable. Convert at the display boundary.
type Time int64

// MinTime and MaxTime bound the representable range used for open intervals.
const (
	MinTime Time = -1 << 62
	MaxTime Time = 1<<62 - 1
)

// Millis returns the raw millisecond value.
func (t Time) Millis() int64 { return int64(t) }

// Std converts to a time.Time in UTC. Display and calendar work only.
func (t Time) Std() time.Time { return time.UnixMilli(int64(t)).UTC() }

// FromStd converts a time.Time to a Time.
func FromStd(t time.Time) Time { return Time(t.UnixMilli()) }

// MustParseTime parses an RFC3339 timestamp, returning zero on failure. Test
// and fixture use only.
func MustParseTime(s string) Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}
	return FromStd(t)
}

// floorDiv divides a by b rounding toward negative infinity. Go's integer
// division truncates toward zero, which misaligns pre-epoch timestamps.
func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Duration is a span of time in milliseconds.
//
// Not time.Duration: spans are compared against Time values, which are
// millisecond counts, and a nanosecond type invites silent unit errors at
// every boundary. The serialised form is a unit-suffixed string ("45m", "4h",
// "3d") because a bare number in a saved strategy document is unreadable and
// ambiguous.
type Duration int64

// Common spans, expressed in the same millisecond unit as Time.
const (
	Millisecond Duration = 1
	Second               = 1000 * Millisecond
	Minute               = 60 * Second
	Hour                 = 60 * Minute
	Day                  = 24 * Hour
	Week                 = 7 * Day
)

// ErrUnknownDuration is returned by ParseDuration for an unparseable span.
var ErrUnknownDuration = fmt.Errorf("core: unknown duration")

// Millis returns the raw millisecond value.
func (d Duration) Millis() int64 { return int64(d) }

// TFDuration returns the span of one bar of tf. TFTick returns zero.
func TFDuration(tf TF) Duration { return Duration(tf.Millis()) }

var durationUnits = []struct {
	suffix string
	span   Duration
}{
	{"w", Week},
	{"d", Day},
	{"h", Hour},
	{"m", Minute},
	{"s", Second},
	{"ms", Millisecond},
}

// String returns the canonical form: the largest whole unit that divides the
// span exactly, so 14400000 renders as "4h" rather than "240m".
func (d Duration) String() string {
	if d == 0 {
		return "0"
	}
	for _, u := range durationUnits {
		if d%u.span == 0 {
			return strconv.FormatInt(int64(d/u.span), 10) + u.suffix
		}
	}
	return strconv.FormatInt(int64(d), 10) + "ms"
}

// ParseDuration parses a canonical duration. Units are w, d, h, m, s and ms;
// a compound form ("1h30m") is deliberately not accepted, because every window
// this project expresses is a single unit and accepting more invites the
// ambiguity the type exists to remove.
func ParseDuration(s string) (Duration, error) {
	if s == "0" {
		return 0, nil
	}
	for _, u := range durationUnits {
		digits, ok := strings.CutSuffix(s, u.suffix)
		if !ok || digits == "" {
			continue
		}
		n, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			continue
		}
		return Duration(n) * u.span, nil
	}
	return 0, fmt.Errorf("%w: %q", ErrUnknownDuration, s)
}

// MarshalJSON writes the canonical unit-suffixed form.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// UnmarshalJSON accepts the canonical form. A bare number is rejected rather
// than assigned a unit: guessing milliseconds when the author meant minutes
// would be wrong by 60,000x and silent.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return fmt.Errorf("duration: decode: expected a unit-suffixed string such as \"45m\"")
	}
	parsed, err := ParseDuration(name)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

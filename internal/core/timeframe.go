package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TF is a timeframe. The set is closed: an open string type invites "m15"
// versus "M15" versus "15m", and prevents the ordering comparisons the
// no-look-ahead rules depend on.
type TF uint8

const (
	TFTick TF = iota
	TFS1
	TFM1
	TFM5
	TFM15
	TFM30
	TFH1
	TFH4
	TFD1
	TFW1
	tfCount
)

const (
	secondMS = 1000
	minuteMS = 60 * secondMS
	hourMS   = 60 * minuteMS
	dayMS    = 24 * hourMS
	weekMS   = 7 * dayMS
)

var tfMillis = [tfCount]int64{
	TFTick: 0,
	TFS1:   secondMS,
	TFM1:   minuteMS,
	TFM5:   5 * minuteMS,
	TFM15:  15 * minuteMS,
	TFM30:  30 * minuteMS,
	TFH1:   hourMS,
	TFH4:   4 * hourMS,
	TFD1:   dayMS,
	TFW1:   weekMS,
}

var tfNames = [tfCount]string{
	TFTick: "tick",
	TFS1:   "s1",
	TFM1:   "m1",
	TFM5:   "m5",
	TFM15:  "m15",
	TFM30:  "m30",
	TFH1:   "h1",
	TFH4:   "h4",
	TFD1:   "d1",
	TFW1:   "w1",
}

// ErrUnknownTimeframe is returned by ParseTF for an unrecognised string.
var ErrUnknownTimeframe = fmt.Errorf("core: unknown timeframe")

// Millis returns the timeframe's duration in milliseconds. TFTick returns 0.
func (tf TF) Millis() int64 {
	if tf >= tfCount {
		return 0
	}
	return tfMillis[tf]
}

// String returns the canonical lower-case name, which is also the
// serialisation form.
func (tf TF) String() string {
	if tf >= tfCount {
		return "invalid"
	}
	return tfNames[tf]
}

// Valid reports whether tf is a known timeframe.
func (tf TF) Valid() bool { return tf < tfCount }

// ParseTF parses a canonical timeframe name.
func ParseTF(s string) (TF, error) {
	for i, n := range tfNames {
		if n == s {
			return TF(i), nil
		}
	}
	return 0, fmt.Errorf("%w: %q", ErrUnknownTimeframe, s)
}

// MarshalJSON exposes canonical timeframe names at the strategy boundary. The
// enum's numeric values are an implementation detail and should never leak
// into a hand-authored strategy document.
func (tf TF) MarshalJSON() ([]byte, error) {
	return json.Marshal(tf.String())
}

// UnmarshalJSON accepts the case-insensitive names used by browser clients
// (for example "M15") and normalises them to the closed TF enum.
func (tf *TF) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		parsed, parseErr := ParseTF(strings.ToLower(name))
		if parseErr != nil {
			return parseErr
		}
		*tf = parsed
		return nil
	}
	// Numeric values appeared in early fixture and persisted strategy JSON.
	// Continue accepting only the closed enum range while serialising modern
	// documents as readable names.
	var legacy uint8
	if err := json.Unmarshal(data, &legacy); err != nil || TF(legacy) >= tfCount {
		return fmt.Errorf("timeframe: decode: expected a timeframe name or legacy enum")
	}
	*tf = TF(legacy)
	return nil
}

// Finer reports whether tf is strictly finer than o. TFTick is finer than
// everything.
func (tf TF) Finer(o TF) bool {
	if tf == TFTick {
		return o != TFTick
	}
	if o == TFTick {
		return false
	}
	return tf.Millis() < o.Millis()
}

// Coarser reports whether tf is strictly coarser than o.
func (tf TF) Coarser(o TF) bool { return o.Finer(tf) }

// weekAnchorMS shifts the epoch so that week alignment lands on Monday.
// 1970-01-01 was a Thursday; the preceding Monday is three days earlier.
const weekAnchorMS = 3 * dayMS

// AlignDown returns the open time of the bar of this timeframe containing t.
// Alignment is UTC-based. Session-relative day boundaries — an FX day opening
// at 17:00 New York, for instance — are an instrument.Calendar concern, not a
// timeframe one.
func (tf TF) AlignDown(t Time) Time {
	d := tf.Millis()
	if d == 0 {
		return t
	}
	if tf == TFW1 {
		v := int64(t) + weekAnchorMS
		return Time(floorDiv(v, d)*d - weekAnchorMS)
	}
	return Time(floorDiv(int64(t), d) * d)
}

// Next returns the open time of the bar following the one containing t.
func (tf TF) Next(t Time) Time {
	d := tf.Millis()
	if d == 0 {
		return t
	}
	return tf.AlignDown(t) + Time(d)
}

// CloseOf returns the instant at which the bar opening at open closes. A bar
// is a half-open interval [open, close): a timestamp equal to close belongs to
// the next bar.
func (tf TF) CloseOf(open Time) Time { return open + Time(tf.Millis()) }

// Contains reports whether t falls within the bar opening at open.
func (tf TF) Contains(open, t Time) bool {
	return t >= open && t < tf.CloseOf(open)
}

package core

import (
	"errors"
	"sort"
)

// ErrRaggedSeries is returned when a Series' columns differ in length.
var ErrRaggedSeries = errors.New("core: series columns have differing lengths")

// Series is a bar series in struct-of-arrays form.
//
// Not []Bar: a primitive touching only closes then streams 8 bytes per bar
// rather than 48, and indicators dominate compute. The ergonomic cost is real
// and is repaid several times over.
//
// A Series is immutable once published. Sweeps share one by pointer across
// many goroutines; mutating a published Series is a data race and a
// determinism bug.
type Series struct {
	Symbol string
	TF     TF

	Time   []Time
	Open   []Price
	High   []Price
	Low    []Price
	Close  []Price
	Volume []int64
}

// NewSeries allocates a Series with capacity for n bars and zero length.
func NewSeries(symbol string, tf TF, n int) *Series {
	return &Series{
		Symbol: symbol,
		TF:     tf,
		Time:   make([]Time, 0, n),
		Open:   make([]Price, 0, n),
		High:   make([]Price, 0, n),
		Low:    make([]Price, 0, n),
		Close:  make([]Price, 0, n),
		Volume: make([]int64, 0, n),
	}
}

// Len returns the number of bars.
func (s *Series) Len() int { return len(s.Time) }

// Append adds one bar. Construction only — never call this on a published
// Series.
func (s *Series) Append(t Time, o, h, l, c Price, v int64) {
	s.Time = append(s.Time, t)
	s.Open = append(s.Open, o)
	s.High = append(s.High, h)
	s.Low = append(s.Low, l)
	s.Close = append(s.Close, c)
	s.Volume = append(s.Volume, v)
}

// Validate checks structural invariants: equal column lengths, strictly
// increasing timestamps aligned to the timeframe, and high/low bracketing
// open and close.
func (s *Series) Validate() error {
	n := len(s.Time)
	if len(s.Open) != n || len(s.High) != n || len(s.Low) != n ||
		len(s.Close) != n || len(s.Volume) != n {
		return ErrRaggedSeries
	}
	for i := 0; i < n; i++ {
		if i > 0 && s.Time[i] <= s.Time[i-1] {
			return errors.New("core: series timestamps are not strictly increasing")
		}
		if s.TF != TFTick && s.TF.AlignDown(s.Time[i]) != s.Time[i] {
			return errors.New("core: series timestamp is not aligned to its timeframe")
		}
		if s.High[i] < s.Low[i] {
			return errors.New("core: series bar has high below low")
		}
		if s.Open[i] > s.High[i] || s.Open[i] < s.Low[i] ||
			s.Close[i] > s.High[i] || s.Close[i] < s.Low[i] {
			return errors.New("core: series bar has open or close outside its range")
		}
	}
	return nil
}

// IndexAt returns the index of the bar whose open time is exactly t.
func (s *Series) IndexAt(t Time) (int, bool) {
	i := sort.Search(len(s.Time), func(i int) bool { return s.Time[i] >= t })
	if i < len(s.Time) && s.Time[i] == t {
		return i, true
	}
	return 0, false
}

// IndexBefore returns the index of the last bar opening at or before t.
func (s *Series) IndexBefore(t Time) (int, bool) {
	i := sort.Search(len(s.Time), func(i int) bool { return s.Time[i] > t })
	if i == 0 {
		return 0, false
	}
	return i - 1, true
}

// Slice returns a view of the bars with open times in [from, to). The returned
// Series shares backing arrays with s and must be treated as equally
// immutable — no copy is made, which is the point.
func (s *Series) Slice(from, to Time) *Series {
	lo := sort.Search(len(s.Time), func(i int) bool { return s.Time[i] >= from })
	hi := sort.Search(len(s.Time), func(i int) bool { return s.Time[i] >= to })
	if hi < lo {
		hi = lo
	}
	return &Series{
		Symbol: s.Symbol,
		TF:     s.TF,
		Time:   s.Time[lo:hi:hi],
		Open:   s.Open[lo:hi:hi],
		High:   s.High[lo:hi:hi],
		Low:    s.Low[lo:hi:hi],
		Close:  s.Close[lo:hi:hi],
		Volume: s.Volume[lo:hi:hi],
	}
}

// SliceInto writes a view of [from, to) into dst, allocating nothing. The hot
// path uses this; Slice is the convenience form for callers where one small
// header allocation does not matter.
func (s *Series) SliceInto(dst *Series, from, to Time) {
	lo := sort.Search(len(s.Time), func(i int) bool { return s.Time[i] >= from })
	hi := sort.Search(len(s.Time), func(i int) bool { return s.Time[i] >= to })
	if hi < lo {
		hi = lo
	}
	dst.Symbol = s.Symbol
	dst.TF = s.TF
	dst.Time = s.Time[lo:hi:hi]
	dst.Open = s.Open[lo:hi:hi]
	dst.High = s.High[lo:hi:hi]
	dst.Low = s.Low[lo:hi:hi]
	dst.Close = s.Close[lo:hi:hi]
	dst.Volume = s.Volume[lo:hi:hi]
}

// Bounds returns the first and last open times. Reports false when empty.
func (s *Series) Bounds() (first, last Time, ok bool) {
	if len(s.Time) == 0 {
		return 0, 0, false
	}
	return s.Time[0], s.Time[len(s.Time)-1], true
}

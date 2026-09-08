package timeofday

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"google.golang.org/protobuf/proto"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
)

type bar struct {
	t                       string
	open, high, low, close_ core.Price
	volume                  int64
}

func seriesOf(tf core.TF, bars []bar) *core.Series {
	s := core.NewSeries("X", tf, len(bars))
	for _, b := range bars {
		s.Append(core.MustParseTime(b.t), b.open, b.high, b.low, b.close_, b.volume)
	}
	return s
}

func evalRef(t *testing.T, typ string, params proto.Message, tf core.TF, s *core.Series) primitive.Value {
	t.Helper()
	p, err := primitive.DefaultRegistry.Get("timeofday", typ)
	if err != nil {
		t.Fatal(err)
	}
	v, _, err := primitive.Evaluate(context.Background(), p, params, tf, s, "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Validate(s.Len()); err != nil {
		t.Fatalf("%s produced a value the contract rejects: %v", typ, err)
	}
	return v
}

func assertLevels(t *testing.T, v primitive.Value, want []core.Price, valid []bool) {
	t.Helper()
	for i := range want {
		if v.Valid.Get(i) != valid[i] || (valid[i] && v.Level[i] != want[i]) {
			t.Fatalf("bar %d = (%d, %t), want (%d, %t)", i, v.Level[i], v.Valid.Get(i), want[i], valid[i])
		}
	}
}

func TestPeriodExtremeLookback(t *testing.T) {
	s := seriesOf(core.TFD1, []bar{
		{t: "2025-01-01T00:00:00Z", high: 10, low: 2},
		{t: "2025-01-02T00:00:00Z", high: 12, low: 4},
		{t: "2025-01-03T00:00:00Z", high: 9, low: 1},
	})
	cases := []struct {
		typ       string
		lookback  uint32
		want      []core.Price
		wantValid []bool
	}{
		{"period_high", 0, []core.Price{10, 12, 9}, []bool{true, true, true}},
		{"period_low", 0, []core.Price{2, 4, 1}, []bool{true, true, true}},
		// A lookback the series cannot satisfy is invalid, never zero-filled.
		{"period_high", 1, []core.Price{0, 10, 12}, []bool{false, true, true}},
		{"period_low", 2, []core.Price{0, 0, 2}, []bool{false, false, true}},
	}
	for _, c := range cases {
		t.Run(c.typ, func(t *testing.T) {
			v := evalRef(t, c.typ, &assayv1.PeriodExtremeParams{Lookback: c.lookback}, core.TFD1, s)
			assertLevels(t, v, c.want, c.wantValid)
		})
	}
}

// The point of the detector: a daily node read from a fine chart is the
// previous day's high, because a fine bar only ever sees closed daily bars.
func TestPeriodHighProjectsToPreviousDayHigh(t *testing.T) {
	daily := seriesOf(core.TFD1, []bar{
		{t: "2025-01-01T00:00:00Z", high: 10, low: 2},
		{t: "2025-01-02T00:00:00Z", high: 12, low: 4},
	})
	v := evalRef(t, "period_high", &assayv1.PeriodExtremeParams{}, core.TFD1, daily)

	fine := seriesOf(core.TFH1, []bar{
		{t: "2025-01-01T12:00:00Z", high: 5, low: 5},
		{t: "2025-01-02T12:00:00Z", high: 5, low: 5},
		{t: "2025-01-03T12:00:00Z", high: 5, low: 5},
	})
	projected, err := primitive.Project(v, daily, fine, primitive.StalePolicy{Hold: true})
	if err != nil {
		t.Fatal(err)
	}
	// Midway through 1 Jan no daily bar has closed; on 2 Jan the 1 Jan high of
	// 10 is visible; on 3 Jan the 2 Jan high of 12 is.
	assertLevels(t, projected, []core.Price{0, 10, 12}, []bool{false, true, true})
}

func TestPeriodExtremeRejectsAnAbsurdLookback(t *testing.T) {
	p, err := primitive.DefaultRegistry.Get("timeofday", "period_high")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.New(&assayv1.PeriodExtremeParams{Lookback: 5000}, core.TFD1); err == nil {
		t.Fatal("a lookback beyond any plausible series was accepted")
	}
}

func TestOpeningRangeIsInvalidWhileItForms(t *testing.T) {
	// A 30-minute range from 09:00, on M15 bars, across two days.
	s := seriesOf(core.TFM15, []bar{
		{t: "2025-01-01T08:45:00Z", high: 20, low: 1},
		{t: "2025-01-01T09:00:00Z", high: 10, low: 4},
		{t: "2025-01-01T09:15:00Z", high: 12, low: 6},
		{t: "2025-01-01T09:30:00Z", high: 30, low: 0},
		{t: "2025-01-01T10:00:00Z", high: 31, low: 0},
		{t: "2025-01-02T09:00:00Z", high: 40, low: 21},
		{t: "2025-01-02T09:15:00Z", high: 41, low: 20},
		{t: "2025-01-02T09:30:00Z", high: 99, low: 0},
	})
	params := &assayv1.OpeningRangeParams{StartMinuteUtc: 9 * 60, RangeMinutes: 30}

	high := evalRef(t, "opening_range_high", params, core.TFM15, s)
	// Nothing before the first range closes; the 09:00 and 09:15 bars are the
	// range itself and must not publish it while it is still forming.
	assertLevels(t, high,
		[]core.Price{0, 0, 0, 12, 12, 0, 0, 41},
		[]bool{false, false, false, true, true, false, false, true})

	low := evalRef(t, "opening_range_low", params, core.TFM15, s)
	assertLevels(t, low,
		[]core.Price{0, 0, 0, 4, 4, 0, 0, 20},
		[]bool{false, false, false, true, true, false, false, true})
}

func TestOpeningRangeRefusesToCrossMidnight(t *testing.T) {
	p, err := primitive.DefaultRegistry.Get("timeofday", "opening_range_high")
	if err != nil {
		t.Fatal(err)
	}
	for _, params := range []*assayv1.OpeningRangeParams{
		{StartMinuteUtc: 23 * 60, RangeMinutes: 120},
		{StartMinuteUtc: 9 * 60, RangeMinutes: 0},
		{StartMinuteUtc: 2000, RangeMinutes: 30},
	} {
		if _, err := p.New(params, core.TFM15); err == nil {
			t.Fatalf("accepted an impossible opening range: %v", params)
		}
	}
}

func TestSessionVWAPWeightsByVolume(t *testing.T) {
	// Typical prices of 6 and 12 with weights 1 and 3 average to 10.5, which
	// truncates to 10 in ticks.
	s := seriesOf(core.TFH1, []bar{
		{t: "2025-01-01T07:00:00Z", high: 99, low: 99, close_: 99, volume: 5},
		{t: "2025-01-01T08:00:00Z", high: 6, low: 6, close_: 6, volume: 1},
		{t: "2025-01-01T09:00:00Z", high: 12, low: 12, close_: 12, volume: 3},
		{t: "2025-01-01T11:00:00Z", high: 50, low: 50, close_: 50, volume: 9},
	})
	v := evalRef(t, "session_vwap", &assayv1.VwapParams{StartMinuteUtc: 8 * 60, EndMinuteUtc: 10 * 60}, core.TFH1, s)
	assertLevels(t, v,
		[]core.Price{0, 6, 10, 0},
		[]bool{false, true, true, false})
}

// A new session starts its own average. Yesterday's cannot leak into today.
func TestSessionVWAPResetsEachSession(t *testing.T) {
	s := seriesOf(core.TFH1, []bar{
		{t: "2025-01-01T08:00:00Z", high: 30, low: 30, close_: 30, volume: 100},
		{t: "2025-01-01T09:00:00Z", high: 30, low: 30, close_: 30, volume: 100},
		{t: "2025-01-02T08:00:00Z", high: 9, low: 9, close_: 9, volume: 1},
	})
	v := evalRef(t, "session_vwap", &assayv1.VwapParams{StartMinuteUtc: 8 * 60, EndMinuteUtc: 10 * 60}, core.TFH1, s)
	assertLevels(t, v, []core.Price{30, 30, 9}, []bool{true, true, true})
}

// A zero-volume bar cannot move a weighted average, and must not be given a
// weight of its own to make the arithmetic tidy.
func TestSessionVWAPHoldsThroughZeroVolumeBars(t *testing.T) {
	s := seriesOf(core.TFH1, []bar{
		{t: "2025-01-01T08:00:00Z", high: 10, low: 10, close_: 10, volume: 0},
		{t: "2025-01-01T09:00:00Z", high: 20, low: 20, close_: 20, volume: 4},
		{t: "2025-01-01T09:30:00Z", high: 90, low: 90, close_: 90, volume: 0},
	})
	v := evalRef(t, "session_vwap", &assayv1.VwapParams{StartMinuteUtc: 8 * 60, EndMinuteUtc: 10 * 60}, core.TFH1, s)
	// Nothing has traded at the first bar, so there is no average to report.
	assertLevels(t, v, []core.Price{0, 20, 20}, []bool{false, true, true})
}

// A session whose weights no longer fit in fixed point reports nothing rather
// than a wrapped sum, which would look exactly like a price.
func TestSessionVWAPReportsNothingRatherThanOverflow(t *testing.T) {
	s := seriesOf(core.TFH1, []bar{
		{t: "2025-01-01T08:00:00Z", high: 10, low: 10, close_: 10, volume: 10},
		{t: "2025-01-01T09:00:00Z", high: math.MaxInt32, low: math.MaxInt32, close_: math.MaxInt32, volume: math.MaxInt64 / 4},
	})
	v := evalRef(t, "session_vwap", &assayv1.VwapParams{StartMinuteUtc: 8 * 60, EndMinuteUtc: 10 * 60}, core.TFH1, s)
	if !v.Valid.Get(0) || v.Level[0] != 10 {
		t.Fatalf("the first bar should be a plain average: (%d, %t)", v.Level[0], v.Valid.Get(0))
	}
	if v.Valid.Get(1) {
		t.Fatalf("an overflowing session published a level: %d", v.Level[1])
	}
}

func TestSessionVWAPRejectsAnEmptySession(t *testing.T) {
	p, err := primitive.DefaultRegistry.Get("timeofday", "session_vwap")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.New(&assayv1.VwapParams{StartMinuteUtc: 480, EndMinuteUtc: 480}, core.TFH1); err == nil {
		t.Fatal("a session that opens and closes at the same minute was accepted")
	}
}

// Short and empty series must not panic or emit a value they cannot support.
func TestReferencePrimitivesHandleShortSeries(t *testing.T) {
	cases := []struct {
		typ    string
		params proto.Message
	}{
		{"period_high", &assayv1.PeriodExtremeParams{Lookback: 3}},
		{"period_low", &assayv1.PeriodExtremeParams{Lookback: 3}},
		{"opening_range_high", &assayv1.OpeningRangeParams{StartMinuteUtc: 540, RangeMinutes: 30}},
		{"opening_range_low", &assayv1.OpeningRangeParams{StartMinuteUtc: 540, RangeMinutes: 30}},
		{"session_vwap", &assayv1.VwapParams{StartMinuteUtc: 480, EndMinuteUtc: 600}},
	}
	for _, c := range cases {
		t.Run(c.typ, func(t *testing.T) {
			for _, n := range []int{0, 1} {
				bars := make([]bar, n)
				for i := range bars {
					bars[i] = bar{t: "2025-01-01T09:00:00Z", high: 2, low: 1, close_: 1, volume: 1}
				}
				v := evalRef(t, c.typ, c.params, core.TFH1, seriesOf(core.TFH1, bars))
				if v.Len() != n {
					t.Fatalf("%d bars in, %d values out", n, v.Len())
				}
			}
		})
	}
}

// The determinism gate: the same input twice must be byte-identical.
func TestReferencePrimitivesAreDeterministic(t *testing.T) {
	s := seriesOf(core.TFM15, []bar{
		{t: "2025-01-01T09:00:00Z", high: 10, low: 4, close_: 8, volume: 3},
		{t: "2025-01-01T09:15:00Z", high: 12, low: 6, close_: 7, volume: 5},
		{t: "2025-01-01T09:30:00Z", high: 30, low: 0, close_: 20, volume: 7},
		{t: "2025-01-02T09:00:00Z", high: 40, low: 21, close_: 33, volume: 11},
		{t: "2025-01-02T09:30:00Z", high: 41, low: 20, close_: 25, volume: 13},
	})
	cases := []struct {
		typ    string
		params proto.Message
	}{
		{"period_high", &assayv1.PeriodExtremeParams{Lookback: 1}},
		{"period_low", &assayv1.PeriodExtremeParams{}},
		{"opening_range_high", &assayv1.OpeningRangeParams{StartMinuteUtc: 540, RangeMinutes: 30}},
		{"opening_range_low", &assayv1.OpeningRangeParams{StartMinuteUtc: 540, RangeMinutes: 30}},
		{"session_vwap", &assayv1.VwapParams{StartMinuteUtc: 540, EndMinuteUtc: 600}},
	}
	for _, c := range cases {
		t.Run(c.typ, func(t *testing.T) {
			first := encodeLevels(t, evalRef(t, c.typ, c.params, core.TFM15, s))
			second := encodeLevels(t, evalRef(t, c.typ, c.params, core.TFM15, s))
			if string(first) != string(second) {
				t.Fatalf("the same input produced different output:\n%s\n%s", first, second)
			}
		})
	}
}

func encodeLevels(t *testing.T, v primitive.Value) []byte {
	t.Helper()
	out := struct {
		Level []core.Price `json:"level"`
		Valid []bool       `json:"valid"`
	}{Level: v.Level}
	for i := 0; i < v.Len(); i++ {
		out.Valid = append(out.Valid, v.Valid.Get(i))
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func benchSeries(n int) *core.Series {
	s := core.NewSeries("X", core.TFM15, n)
	start := core.MustParseTime("2025-01-01T00:00:00Z")
	for i := 0; i < n; i++ {
		p := core.Price(100 + i%50)
		s.Append(start+core.Time(int64(i)*core.TFM15.Millis()), p, p+2, p-2, p, int64(i%97)+1)
	}
	return s
}

func BenchmarkReferencePrimitives(b *testing.B) {
	s := benchSeries(20_000)
	cases := []struct {
		typ    string
		params proto.Message
	}{
		{"period_high", &assayv1.PeriodExtremeParams{Lookback: 1}},
		{"opening_range_high", &assayv1.OpeningRangeParams{StartMinuteUtc: 540, RangeMinutes: 30}},
		{"session_vwap", &assayv1.VwapParams{StartMinuteUtc: 540, EndMinuteUtc: 960}},
	}
	for _, c := range cases {
		p, err := primitive.DefaultRegistry.Get("timeofday", c.typ)
		if err != nil {
			b.Fatal(err)
		}
		inst, err := p.New(c.params, core.TFM15)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(c.typ, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := inst.Eval(nil, s); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// A day with no bars inside the window leaves the previous completed range as
// the most recent one. That is the honest reading — it is the last opening
// range that exists — but it is worth pinning, because it is the one case where
// the published level is older than the current day.
func TestOpeningRangeSurvivesADayWithNoWindowBars(t *testing.T) {
	s := seriesOf(core.TFM15, []bar{
		{t: "2025-01-01T09:00:00Z", high: 10, low: 4},
		{t: "2025-01-01T09:30:00Z", high: 30, low: 0},
		// 2 January has no bars between 09:00 and 09:30 at all.
		{t: "2025-01-02T14:00:00Z", high: 50, low: 40},
		{t: "2025-01-03T09:00:00Z", high: 60, low: 55},
		{t: "2025-01-03T09:30:00Z", high: 70, low: 50},
	})
	params := &assayv1.OpeningRangeParams{StartMinuteUtc: 9 * 60, RangeMinutes: 30}
	v := evalRef(t, "opening_range_high", params, core.TFM15, s)
	assertLevels(t, v,
		// Bar 2 still reports 1 January's range; bar 3 is 3 January's window
		// re-forming, so nothing is published; bar 4 is the new range.
		[]core.Price{0, 10, 10, 0, 60},
		[]bool{false, true, true, false, true})
}

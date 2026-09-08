package timeofday

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
)

var update = flag.Bool("update", false, "rewrite the golden reference fixture")

// goldenSeries is the fixture's bar form: integer ticks and UTC timestamps, so
// a reviewer can read the expected levels off the prices by eye.
type goldenSeries struct {
	Time   string     `json:"time"`
	Open   core.Price `json:"open"`
	High   core.Price `json:"high"`
	Low    core.Price `json:"low"`
	Close  core.Price `json:"close"`
	Volume int64      `json:"volume"`
}

type goldenCase struct {
	Type   string           `json:"type"`
	Params map[string]int64 `json:"params"`
	Level  []core.Price     `json:"level"`
	Valid  []bool           `json:"valid"`
}

type goldenFile struct {
	Timeframe string         `json:"timeframe"`
	Bars      []goldenSeries `json:"bars"`
	Cases     []goldenCase   `json:"cases"`
}

// goldenFixture pins the exact levels and, just as importantly, the exact bars
// on which each detector publishes nothing. The invalid boundaries are the part
// most likely to drift without anyone noticing.
func TestReferenceGolden(t *testing.T) {
	path := filepath.Join("testdata", "golden", "reference.golden.json")
	bars := []goldenSeries{
		{"2025-01-01T08:45:00Z", 5, 20, 1, 5, 2},
		{"2025-01-01T09:00:00Z", 5, 10, 4, 8, 3},
		{"2025-01-01T09:15:00Z", 8, 12, 6, 7, 5},
		{"2025-01-01T09:30:00Z", 7, 30, 0, 20, 7},
		{"2025-01-01T10:00:00Z", 20, 31, 0, 25, 11},
		{"2025-01-02T09:00:00Z", 25, 40, 21, 33, 13},
		{"2025-01-02T09:15:00Z", 33, 41, 20, 25, 17},
		{"2025-01-02T09:30:00Z", 25, 99, 0, 50, 19},
	}
	s := core.NewSeries("X", core.TFM15, len(bars))
	for _, b := range bars {
		s.Append(core.MustParseTime(b.Time), b.Open, b.High, b.Low, b.Close, b.Volume)
	}

	specs := []struct {
		typ    string
		params proto.Message
		fields map[string]int64
	}{
		{"period_high", &assayv1.PeriodExtremeParams{}, map[string]int64{"lookback": 0}},
		{"period_low", &assayv1.PeriodExtremeParams{Lookback: 1}, map[string]int64{"lookback": 1}},
		{
			"opening_range_high",
			&assayv1.OpeningRangeParams{StartMinuteUtc: 540, RangeMinutes: 30},
			map[string]int64{"startMinuteUtc": 540, "rangeMinutes": 30},
		},
		{
			"opening_range_low",
			&assayv1.OpeningRangeParams{StartMinuteUtc: 540, RangeMinutes: 30},
			map[string]int64{"startMinuteUtc": 540, "rangeMinutes": 30},
		},
		{
			"session_vwap",
			&assayv1.VwapParams{StartMinuteUtc: 540, EndMinuteUtc: 600},
			map[string]int64{"startMinuteUtc": 540, "endMinuteUtc": 600},
		},
	}

	got := goldenFile{Timeframe: core.TFM15.String(), Bars: bars}
	for _, spec := range specs {
		v := evalRef(t, spec.typ, spec.params, core.TFM15, s)
		c := goldenCase{Type: spec.typ, Params: spec.fields, Level: make([]core.Price, v.Len())}
		copy(c.Level, v.Level)
		for i := 0; i < v.Len(); i++ {
			c.Valid = append(c.Valid, v.Valid.Get(i))
			// A bar that publishes nothing carries no level either, so the
			// fixture never records a number a run would not have used.
			if !c.Valid[i] {
				c.Level[i] = 0
			}
		}
		got.Cases = append(got.Cases, c)
	}

	encoded, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if *update {
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden fixture rewritten")
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/packs/timeofday -update to create it)", err)
	}
	if string(want) != string(encoded) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, encoded)
	}
}

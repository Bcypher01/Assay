package eventstudy

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/event"
)

const MethodologyVersion = "v2"

type Request struct {
	Events      event.Snapshot
	Series      *core.Series
	Filter      event.Filter
	Windows     []core.TF
	BarSnapshot string
	Trade       *TradeConfig
}
type Exclusion struct{ EventID, Reason string }
type Observation struct {
	EventID                string
	OccurredAt, AlignedBar core.Time
	Returns                map[string]float64
	Actual, Forecast       string
	Previous               string
	Surprise               float64
	SurpriseZ              float64
	HasSurprise            bool
}
type WindowSummary struct {
	Window                       string
	Count, UpCount, DownCount    int
	Mean, Median                 float64
	MeanAbs, MedianAbs           float64
	BaselineMeanAbs, ImpactRatio float64
}
type Result struct {
	EventSnapshot, BarSnapshot, MethodologyVersion string
	Eligible                                       []Observation
	Excluded                                       []Exclusion
	Windows                                        []WindowSummary
	TradeSummary                                   *TradeSummary
	Trades                                         []Trade
}

// Run aligns exact-timestamp events to the first bar at or after KnowledgeAt.
// A requested window is measured from that bar's open to its closing bar.
func Run(req Request) (Result, error) {
	if req.Series == nil || req.Series.Len() == 0 {
		return Result{}, fmt.Errorf("eventstudy: missing series")
	}
	if len(req.Windows) == 0 {
		return Result{}, fmt.Errorf("eventstudy: no windows")
	}
	for _, window := range req.Windows {
		if window.Finer(req.Series.TF) {
			return Result{}, fmt.Errorf("eventstudy: %s window requires %s bars or finer", window, window)
		}
	}
	if err := req.Series.Validate(); err != nil {
		return Result{}, err
	}
	out := Result{EventSnapshot: req.Events.Manifest.ContentHash, BarSnapshot: req.BarSnapshot, MethodologyVersion: MethodologyVersion}
	values := make(map[core.TF][]float64, len(req.Windows))
	absValues := make(map[core.TF][]float64, len(req.Windows))
	baselineValues := make(map[core.TF][]float64, len(req.Windows))
	cohort := req.Events.Query(req.Filter)
	surprises := surpriseHistory(cohort)
	for _, e := range cohort {
		if e.Precision != event.PrecisionExact {
			out.Excluded = append(out.Excluded, Exclusion{e.ID, "timestamp precision is not exact"})
			continue
		}
		i := sort.Search(len(req.Series.Time), func(i int) bool { return req.Series.Time[i] >= e.KnowledgeAt })
		if i == len(req.Series.Time) {
			out.Excluded = append(out.Excluded, Exclusion{e.ID, "no tradable bar after knowledge time"})
			continue
		}
		surprise := surprises[e.ID]
		ob := Observation{
			EventID: e.ID, OccurredAt: e.OccurredAt, AlignedBar: req.Series.Time[i],
			Returns: make(map[string]float64, len(req.Windows)), Actual: decimalString(e.Actual),
			Forecast: decimalString(e.Forecast), Previous: decimalString(e.Previous),
			Surprise: surprise.Value, SurpriseZ: surprise.Z, HasSurprise: surprise.OK,
		}
		complete := true
		for _, w := range req.Windows {
			end := req.Series.Time[i] + core.Time(w.Millis())
			j := sort.Search(len(req.Series.Time), func(k int) bool { return req.Series.Time[k] >= end })
			if j == len(req.Series.Time) || req.Series.Open[i] == 0 {
				complete = false
				break
			}
			v := float64(req.Series.Open[j]-req.Series.Open[i]) / float64(req.Series.Open[i])
			ob.Returns[w.String()] = v
		}
		if !complete {
			out.Excluded = append(out.Excluded, Exclusion{e.ID, "insufficient bars for requested forward window"})
			continue
		}
		for _, w := range req.Windows {
			value := ob.Returns[w.String()]
			values[w] = append(values[w], value)
			absValues[w] = append(absValues[w], math.Abs(value))
			baselineValues[w] = append(baselineValues[w], baselines(req.Series, i, w)...)
		}
		out.Eligible = append(out.Eligible, ob)
	}
	for _, w := range req.Windows {
		x := values[w]
		row := WindowSummary{
			Window: w.String(), Count: len(x), Mean: mean(x), Median: median(x),
			MeanAbs: mean(absValues[w]), MedianAbs: median(absValues[w]), BaselineMeanAbs: mean(baselineValues[w]),
		}
		for _, value := range x {
			if value > 0 {
				row.UpCount++
			} else if value < 0 {
				row.DownCount++
			}
		}
		if row.BaselineMeanAbs > 0 {
			row.ImpactRatio = row.MeanAbs / row.BaselineMeanAbs
		}
		out.Windows = append(out.Windows, row)
	}
	if req.Trade != nil {
		trades, summary, excluded, err := simulate(req, cohort, surprises)
		if err != nil {
			return Result{}, err
		}
		out.Trades, out.TradeSummary = trades, &summary
		out.Excluded = append(out.Excluded, excluded...)
	}
	return out, nil
}

type surpriseReading struct {
	Value, Z float64
	OK       bool
}

// surpriseHistory standardizes only against earlier releases in the cohort.
// A later release can never influence an earlier release's surprise score.
func surpriseHistory(events []event.Event) map[string]surpriseReading {
	out := make(map[string]surpriseReading, len(events))
	prior := make(map[event.Kind][]float64)
	for _, item := range events {
		actual, aok := decimalFloat(item.Actual)
		forecast, fok := decimalFloat(item.Forecast)
		if !aok || !fok {
			continue
		}
		value := actual - forecast
		history := prior[item.Kind]
		reading := surpriseReading{Value: value, OK: true}
		if len(history) >= 2 {
			m := mean(history)
			var variance float64
			for _, x := range history {
				variance += (x - m) * (x - m)
			}
			variance /= float64(len(history))
			if variance > 0 {
				reading.Z = (value - m) / math.Sqrt(variance)
			}
		}
		out[item.ID] = reading
		prior[item.Kind] = append(history, value)
	}
	return out
}

func decimalFloat(value *event.Decimal) (float64, bool) {
	if value == nil {
		return 0, false
	}
	n, err := strconv.ParseFloat(string(*value), 64)
	return n, err == nil
}

func decimalString(value *event.Decimal) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

// baselines sample the same aligned clock time one to four weeks before an
// event. This provides a comparable ordinary-week movement without using any
// prices that occur after the event being evaluated.
func baselines(series *core.Series, eventIndex int, window core.TF) []float64 {
	if eventIndex >= len(series.Time) || series.Open[eventIndex] == 0 {
		return nil
	}
	var out []float64
	for week := 1; week <= 4; week++ {
		start := series.Time[eventIndex] - core.Time(int64(week)*core.TFW1.Millis())
		i, ok := series.IndexAt(start)
		if !ok || series.Open[i] == 0 {
			continue
		}
		end := start + core.Time(window.Millis())
		j := sort.Search(len(series.Time), func(k int) bool { return series.Time[k] >= end })
		if j == len(series.Time) {
			continue
		}
		out = append(out, math.Abs(float64(series.Open[j]-series.Open[i])/float64(series.Open[i])))
	}
	return out
}
func mean(x []float64) float64 {
	var s float64
	for _, v := range x {
		s += v
	}
	if len(x) == 0 {
		return 0
	}
	return s / float64(len(x))
}
func median(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	c := append([]float64(nil), x...)
	sort.Float64s(c)
	n := len(c) / 2
	if len(c)%2 == 1 {
		return c[n]
	}
	return (c[n-1] + c[n]) / 2
}

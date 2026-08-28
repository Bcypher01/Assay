package eventstudy

import (
	"fmt"
	"sort"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/event"
)

const MethodologyVersion = "v1"

type Request struct {
	Events      event.Snapshot
	Series      *core.Series
	Filter      event.Filter
	Windows     []core.TF
	BarSnapshot string
}
type Exclusion struct{ EventID, Reason string }
type Observation struct {
	EventID                string
	OccurredAt, AlignedBar core.Time
	Returns                map[string]float64
}
type WindowSummary struct {
	Window       string
	Count        int
	Mean, Median float64
}
type Result struct {
	EventSnapshot, BarSnapshot, MethodologyVersion string
	Eligible                                       []Observation
	Excluded                                       []Exclusion
	Windows                                        []WindowSummary
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
	if err := req.Series.Validate(); err != nil {
		return Result{}, err
	}
	out := Result{EventSnapshot: req.Events.Manifest.ContentHash, BarSnapshot: req.BarSnapshot, MethodologyVersion: MethodologyVersion}
	values := make(map[core.TF][]float64, len(req.Windows))
	for _, e := range req.Events.Query(req.Filter) {
		if e.Precision != event.PrecisionExact {
			out.Excluded = append(out.Excluded, Exclusion{e.ID, "timestamp precision is not exact"})
			continue
		}
		i := sort.Search(len(req.Series.Time), func(i int) bool { return req.Series.Time[i] >= e.KnowledgeAt })
		if i == len(req.Series.Time) {
			out.Excluded = append(out.Excluded, Exclusion{e.ID, "no tradable bar after knowledge time"})
			continue
		}
		ob := Observation{EventID: e.ID, OccurredAt: e.OccurredAt, AlignedBar: req.Series.Time[i], Returns: make(map[string]float64, len(req.Windows))}
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
			values[w] = append(values[w], ob.Returns[w.String()])
		}
		out.Eligible = append(out.Eligible, ob)
	}
	for _, w := range req.Windows {
		x := values[w]
		out.Windows = append(out.Windows, WindowSummary{Window: w.String(), Count: len(x), Mean: mean(x), Median: median(x)})
	}
	return out, nil
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

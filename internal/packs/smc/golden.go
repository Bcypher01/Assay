package smc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/primitive"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// GoldenFixture is a portable, annotator-owned SMC window. Prices and times
// use the same fixed-point units as core.Series. One fixture contains one
// detector configuration and one annotator's annotations for one window.
type GoldenFixture struct {
	Version    int               `json:"version"`
	WindowID   string            `json:"window_id"`
	Annotator  string            `json:"annotator"`
	Instrument string            `json:"instrument"`
	Timeframe  core.TF           `json:"timeframe"`
	Primitive  string            `json:"primitive"`
	Params     json.RawMessage   `json:"params"`
	Bars       []GoldenBar       `json:"bars"`
	Objects    []AnnotatedObject `json:"objects"`
}
type GoldenBar struct {
	Time   core.Time  `json:"time"`
	Open   core.Price `json:"open"`
	High   core.Price `json:"high"`
	Low    core.Price `json:"low"`
	Close  core.Price `json:"close"`
	Volume int64      `json:"volume"`
}

// AnnotatedObject is either an event (bar only) or a zone (bar plus bounds).
// Bar is the confirmation bar, never the visual origin of a back-referenced zone.
type AnnotatedObject struct {
	Type string     `json:"type"`
	Bar  int        `json:"bar"`
	Low  core.Price `json:"low"`
	High core.Price `json:"high"`
	Side core.Side  `json:"side"`
}

// Agreement is deliberately count-based. It gives annotators and detector
// authors an auditable common metric before any release threshold is chosen.
type Agreement struct {
	Expected, Detected, Matched int
	Precision, Recall, F1       float64
}
type GoldenReport struct {
	WindowID, Annotator, Primitive string
	Agreement                      Agreement
	Missed, Extra                  []AnnotatedObject
}
type SweepCandidate struct {
	Name   string
	Params json.RawMessage
}
type SweepReport struct {
	Name   string
	Report GoldenReport
}

func LoadGoldenFixture(path string) (GoldenFixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GoldenFixture{}, fmt.Errorf("smc golden: read fixture: %w", err)
	}
	var f GoldenFixture
	if err := json.Unmarshal(data, &f); err != nil {
		return GoldenFixture{}, fmt.Errorf("smc golden: decode fixture: %w", err)
	}
	if err := f.Validate(); err != nil {
		return GoldenFixture{}, err
	}
	return f, nil
}
func (f GoldenFixture) Validate() error {
	if f.Version != 1 || f.WindowID == "" || f.Annotator == "" || f.Instrument == "" || !f.Timeframe.Valid() || f.Primitive == "" || len(f.Bars) == 0 {
		return fmt.Errorf("smc golden: incomplete fixture metadata")
	}
	s := f.Series()
	if err := s.Validate(); err != nil {
		return fmt.Errorf("smc golden: invalid bars: %w", err)
	}
	for _, o := range f.Objects {
		if o.Type != "event" && o.Type != "zone" {
			return fmt.Errorf("smc golden: unknown object type %q", o.Type)
		}
		if o.Bar < 0 || o.Bar >= len(f.Bars) {
			return fmt.Errorf("smc golden: object bar out of range")
		}
		if o.Type == "zone" && o.Low > o.High {
			return fmt.Errorf("smc golden: inverted zone")
		}
	}
	return nil
}
func (f GoldenFixture) Series() *core.Series {
	s := core.NewSeries(f.Instrument, f.Timeframe, len(f.Bars))
	for _, b := range f.Bars {
		s.Append(b.Time, b.Open, b.High, b.Low, b.Close, b.Volume)
	}
	return s
}

// Score evaluates the fixture's configured primitive and greedily matches
// predictions to annotations. zoneTolerance is in ticks and is applied to each
// edge, allowing a pilot to state its intended drawing precision explicitly.
func Score(ctx context.Context, f GoldenFixture, zoneTolerance core.Price) (GoldenReport, error) {
	return ScoreWithParams(ctx, f, f.Params, zoneTolerance)
}
func ScoreWithParams(ctx context.Context, f GoldenFixture, raw json.RawMessage, zoneTolerance core.Price) (GoldenReport, error) {
	if err := f.Validate(); err != nil {
		return GoldenReport{}, err
	}
	p, err := primitive.DefaultRegistry.Get("smc", f.Primitive)
	if err != nil {
		return GoldenReport{}, err
	}
	params, err := smcParams(f.Primitive)
	if err != nil {
		return GoldenReport{}, err
	}
	if err := protojson.Unmarshal(raw, params); err != nil {
		return GoldenReport{}, fmt.Errorf("smc golden: decode params: %w", err)
	}
	v, _, err := primitive.Evaluate(ctx, p, params, f.Timeframe, f.Series(), "golden:"+f.WindowID, nil)
	if err != nil {
		return GoldenReport{}, err
	}
	predicted := objectsFromValue(v)
	matched := make([]bool, len(predicted))
	report := GoldenReport{WindowID: f.WindowID, Annotator: f.Annotator, Primitive: f.Primitive}
	report.Agreement.Expected, report.Agreement.Detected = len(f.Objects), len(predicted)
	for _, want := range f.Objects {
		found := -1
		for i, got := range predicted {
			if !matched[i] && match(want, got, zoneTolerance) {
				found = i
				break
			}
		}
		if found < 0 {
			report.Missed = append(report.Missed, want)
		} else {
			matched[found] = true
			report.Agreement.Matched++
		}
	}
	for i, got := range predicted {
		if !matched[i] {
			report.Extra = append(report.Extra, got)
		}
	}
	report.Agreement.Precision, report.Agreement.Recall = ratio(report.Agreement.Matched, report.Agreement.Detected), ratio(report.Agreement.Matched, report.Agreement.Expected)
	if report.Agreement.Precision+report.Agreement.Recall > 0 {
		report.Agreement.F1 = 2 * report.Agreement.Precision * report.Agreement.Recall / (report.Agreement.Precision + report.Agreement.Recall)
	}
	return report, nil
}
func Sweep(ctx context.Context, f GoldenFixture, candidates []SweepCandidate, zoneTolerance core.Price) ([]SweepReport, error) {
	out := make([]SweepReport, 0, len(candidates))
	for _, c := range candidates {
		r, err := ScoreWithParams(ctx, f, c.Params, zoneTolerance)
		if err != nil {
			return nil, fmt.Errorf("smc golden: sweep %s: %w", c.Name, err)
		}
		out = append(out, SweepReport{Name: c.Name, Report: r})
	}
	return out, nil
}

// AnnotationDisagreement returns annotations that are not shared by every
// annotator of a window. It is input data for parameter design, not a verdict
// that any trader's drawing is wrong.
type AnnotationDisagreement struct {
	WindowID               string
	Object                 AnnotatedObject
	PresentBy, MissingFrom []string
}

func Disagreements(fixtures []GoldenFixture, tolerance core.Price) []AnnotationDisagreement {
	byWindow := make(map[string][]GoldenFixture)
	for _, f := range fixtures {
		byWindow[f.WindowID] = append(byWindow[f.WindowID], f)
	}
	var out []AnnotationDisagreement
	for id, group := range byWindow {
		if len(group) < 2 {
			continue
		}
		names := make([]string, 0, len(group))
		for _, f := range group {
			names = append(names, f.Annotator)
		}
		sort.Strings(names)
		for _, f := range group {
			for _, object := range f.Objects {
				present := []string{}
				for _, other := range group {
					for _, candidate := range other.Objects {
						if match(object, candidate, tolerance) {
							present = append(present, other.Annotator)
							break
						}
					}
				}
				if len(present) == len(names) {
					continue
				}
				missing := make([]string, 0, len(names)-len(present))
				for _, n := range names {
					found := false
					for _, p := range present {
						found = found || p == n
					}
					if !found {
						missing = append(missing, n)
					}
				}
				out = append(out, AnnotationDisagreement{WindowID: id, Object: object, PresentBy: present, MissingFrom: missing})
			}
		}
	}
	return out
}
func objectsFromValue(v primitive.Value) []AnnotatedObject {
	out := make([]AnnotatedObject, 0)
	for i := 0; i < v.Len(); i++ {
		if v.Valid == nil || !v.Valid.Get(i) {
			continue
		}
		switch v.Kind {
		case primitive.KindBool:
			if v.Bool != nil && v.Bool.Get(i) {
				out = append(out, AnnotatedObject{Type: "event", Bar: i})
			}
		case primitive.KindZone:
			out = append(out, AnnotatedObject{Type: "zone", Bar: i, Low: v.Zone[i].Low, High: v.Zone[i].High, Side: v.Zone[i].Side})
		}
	}
	return out
}
func match(a, b AnnotatedObject, tolerance core.Price) bool {
	if a.Type != b.Type || a.Bar != b.Bar || a.Side != 0 && b.Side != 0 && a.Side != b.Side {
		return false
	}
	return a.Type == "event" || ((a.Low-b.Low).Abs() <= tolerance && (a.High-b.High).Abs() <= tolerance)
}
func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}
func smcParams(name string) (proto.Message, error) {
	switch name {
	case "market_structure":
		return &assayv1.MarketStructureParams{}, nil
	case "displacement":
		return &assayv1.DisplacementParams{}, nil
	case "fair_value_gap":
		return &assayv1.FairValueGapParams{}, nil
	case "order_block":
		return &assayv1.OrderBlockParams{}, nil
	case "breaker":
		return &assayv1.BreakerParams{}, nil
	case "liquidity_pool":
		return &assayv1.LiquidityPoolParams{}, nil
	case "liquidity_sweep":
		return &assayv1.SweepParams{}, nil
	case "premium_discount":
		return &assayv1.PremiumDiscountParams{}, nil
	default:
		return nil, fmt.Errorf("smc golden: unsupported primitive %q", name)
	}
}

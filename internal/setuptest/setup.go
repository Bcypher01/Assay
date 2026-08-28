// Package setuptest translates a labelled chart drawing into a deterministic
// strategy document and evaluates the resulting rule against prepared bars.
// Drawing coordinates are retained as an example only; historical matching is
// performed by semantic primitives and never repeats an absolute price level.
package setuptest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/Bcypher01/Assay/internal/analytics"
	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/engine"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/ir"
	"github.com/Bcypher01/Assay/internal/primitive"
	"github.com/Bcypher01/Assay/internal/sim"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

type Kind string

const (
	BreakRetest      Kind = "break_retest"
	OrderBlockRetest Kind = "order_block_retest"
	BreakerRetest    Kind = "breaker_retest"
)

// Drawing is the complete user-confirmed meaning of one annotated setup. All
// price fields are instrument ticks at the API boundary.
type Drawing struct {
	Kind                  Kind       `json:"kind"`
	Side                  string     `json:"side"`
	Symbol                string     `json:"symbol"`
	Timeframe             string     `json:"timeframe"`
	AnchorTimeMS          int64      `json:"anchorTimeMs"`
	LevelLow              core.Price `json:"levelLow"`
	LevelHigh             core.Price `json:"levelHigh"`
	Entry                 core.Price `json:"entry"`
	Stop                  core.Price `json:"stop"`
	Target                core.Price `json:"target"`
	LookbackBars          int        `json:"lookbackBars"`
	RetestBars            int        `json:"retestBars"`
	ToleranceTicks        int        `json:"toleranceTicks"`
	ExpiryBars            int        `json:"expiryBars"`
	DisplacementBodyMilli int        `json:"displacementBodyMilli"`
	UseBody               bool       `json:"useBody"`
	ConfirmByClose        bool       `json:"confirmByClose"`
	RiskPerTrade          float64    `json:"riskPerTrade"`
}

type ValidationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ChartRequest struct {
	Symbol    string `json:"symbol"`
	Timeframe string `json:"timeframe"`
	FromMS    int64  `json:"fromMs"`
	ToMS      int64  `json:"toMs"`
	MaxBars   int    `json:"maxBars"`
	Prepare   bool   `json:"prepare"`
}

type Bar struct {
	TimeMS int64      `json:"timeMs"`
	Open   core.Price `json:"open"`
	High   core.Price `json:"high"`
	Low    core.Price `json:"low"`
	Close  core.Price `json:"close"`
	Volume int64      `json:"volume"`
}

type Chart struct {
	Symbol    string `json:"symbol"`
	Timeframe string `json:"timeframe"`
	TickSize  string `json:"tickSize"`
	PipTicks  int64  `json:"pipTicks"`
	Snapshot  string `json:"snapshot"`
	FromMS    int64  `json:"fromMs"`
	ToMS      int64  `json:"toMs"`
	Bars      []Bar  `json:"bars"`
}

type PreviewRequest struct {
	Drawing Drawing `json:"drawing"`
	FromMS  int64   `json:"fromMs"`
	ToMS    int64   `json:"toMs"`
}

type Assumptions struct {
	InitialEquity int64   `json:"initialEquity"`
	RiskPerTrade  float64 `json:"riskPerTrade"`
	SpreadTicks   int64   `json:"spreadTicks"`
	SlippageTicks int64   `json:"slippageTicks"`
}

type RunRequest struct {
	Drawing     Drawing     `json:"drawing"`
	FromMS      int64       `json:"fromMs"`
	ToMS        int64       `json:"toMs"`
	Assumptions Assumptions `json:"assumptions"`
}

func (e ValidationError) Error() string { return e.Path + ": " + e.Message }

func (d Drawing) Validate() []ValidationError {
	var out []ValidationError
	add := func(path, message string) { out = append(out, ValidationError{Path: path, Message: message}) }
	switch d.Kind {
	case BreakRetest, OrderBlockRetest, BreakerRetest:
	default:
		add("kind", "must be break_retest, order_block_retest, or breaker_retest")
	}
	if d.Side != "long" && d.Side != "short" {
		add("side", "must be long or short")
	}
	if d.Symbol == "" {
		add("symbol", "required")
	}
	if _, err := core.ParseTF(d.Timeframe); err != nil {
		add("timeframe", "unsupported timeframe")
	}
	if d.AnchorTimeMS <= 0 {
		add("anchorTimeMs", "select a setup candle")
	}
	if d.LevelLow <= 0 || d.LevelHigh <= 0 || d.LevelLow > d.LevelHigh {
		add("levelLow", "draw a valid level or zone")
	}
	if d.Entry <= 0 || d.Stop <= 0 || d.Target <= 0 {
		add("entry", "entry, stop, and target must be positive prices")
	} else if d.Side == "long" && !(d.Stop < d.Entry && d.Entry < d.Target) {
		add("stop", "a long setup requires stop < entry < target")
	} else if d.Side == "short" && !(d.Target < d.Entry && d.Entry < d.Stop) {
		add("stop", "a short setup requires target < entry < stop")
	}
	if d.LookbackBars < 2 || d.LookbackBars > 500 {
		add("lookbackBars", "must be between 2 and 500")
	}
	if d.RetestBars < 1 || d.RetestBars > 200 {
		add("retestBars", "must be between 1 and 200")
	}
	if d.ToleranceTicks < 0 {
		add("toleranceTicks", "must not be negative")
	}
	if d.ExpiryBars < 1 || d.ExpiryBars > 1000 {
		add("expiryBars", "must be between 1 and 1000")
	}
	if d.DisplacementBodyMilli < 1 || d.DisplacementBodyMilli > 1000 {
		add("displacementBodyMilli", "must be between 1 and 1000")
	}
	if d.RiskPerTrade <= 0 || d.RiskPerTrade > 0.1 || math.IsNaN(d.RiskPerTrade) || math.IsInf(d.RiskPerTrade, 0) {
		add("riskPerTrade", "must be greater than 0 and no more than 10%")
	}
	return out
}

type detectorParams struct {
	LookbackBars          int  `json:"lookbackBars"`
	RetestBars            int  `json:"retestBars"`
	ToleranceTicks        int  `json:"toleranceTicks"`
	ExpiryBars            int  `json:"expiryBars"`
	DisplacementBodyMilli int  `json:"displacementBodyMilli"`
	UseBody               bool `json:"useBody"`
	ConfirmByClose        bool `json:"confirmByClose"`
}

// Compile produces the same IR used by every other Assay authoring surface.
// Absolute drawing coordinates and the sample timestamp intentionally do not
// enter the strategy document or its hash.
func Compile(d Drawing) (*ir.Strategy, error) {
	if errs := d.Validate(); len(errs) > 0 {
		return nil, errs[0]
	}
	tf, _ := core.ParseTF(d.Timeframe)
	params, err := json.Marshal(detectorParams{
		LookbackBars: d.LookbackBars, RetestBars: d.RetestBars,
		ToleranceTicks: d.ToleranceTicks, ExpiryBars: d.ExpiryBars,
		DisplacementBodyMilli: d.DisplacementBodyMilli,
		UseBody:               d.UseBody, ConfirmByClose: d.ConfirmByClose,
	})
	if err != nil {
		return nil, fmt.Errorf("setup test: encode detector parameters: %w", err)
	}
	suffix := "bullish"
	if d.Side == "short" {
		suffix = "bearish"
	}
	entry := ir.EntryBranch{Enabled: true, Name: interpretationRule(d), When: ir.Expression{Node: "setup"}}
	entries := ir.Entries{Timing: ir.EntryTimingNextOpen}
	if d.Side == "long" {
		entries.Long = entry
	} else {
		entries.Short = entry
	}
	stop := (d.Entry - d.Stop).Abs()
	target := (d.Target - d.Entry).Abs()
	return &ir.Strategy{
		Schema:     ir.SchemaV2,
		Name:       title(d),
		Universe:   ir.Universe{Symbols: []string{d.Symbol}},
		Timeframes: map[string]core.TF{"bias": tf, "poi": tf, "entry": tf},
		Risk: ir.Risk{
			Stop:     core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(int64(stop))},
			Target:   core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(int64(target))},
			PerTrade: core.RatioFromFloat(d.RiskPerTrade),
		},
		Template: ir.Template{ID: "semantic-drawing/" + string(d.Kind), Version: 1},
		Entries:  entries,
		Nodes:    []ir.Node{{ID: "setup", Pack: "semantic", Type: string(d.Kind) + "_" + suffix, Params: params, Timeframe: tf}},
	}, nil
}

func title(d Drawing) string {
	name := strings.ReplaceAll(string(d.Kind), "_", " ")
	return strings.ToUpper(d.Side[:1]) + d.Side[1:] + " " + name + " setup"
}

func interpretationRule(d Drawing) string {
	side := "bullish"
	if d.Side == "short" {
		side = "bearish"
	}
	switch d.Kind {
	case BreakRetest:
		return fmt.Sprintf("%s break of the preceding %d-bar extreme and retest within %d bars", side, d.LookbackBars, d.RetestBars)
	case OrderBlockRetest:
		return fmt.Sprintf("first %s retest of an order block created before displacement", side)
	default:
		return fmt.Sprintf("first %s retest after an opposing candle zone becomes a breaker", side)
	}
}

type Match struct {
	SignalTimeMS int64      `json:"signalTimeMs"`
	EntryTimeMS  int64      `json:"entryTimeMs"`
	Entry        core.Price `json:"entry"`
	Stop         core.Price `json:"stop"`
	Target       core.Price `json:"target"`
	Pessimistic  int64      `json:"pessimisticPnl"`
	Optimistic   int64      `json:"optimisticPnl"`
	Ambiguous    bool       `json:"ambiguous"`
}

type Preview struct {
	Snapshot       string            `json:"snapshot"`
	StrategyJSON   json.RawMessage   `json:"strategyJson"`
	StrategyHash   string            `json:"strategyHash"`
	Interpretation string            `json:"interpretation"`
	SampleMatched  bool              `json:"sampleMatched"`
	RuleMatched    bool              `json:"ruleMatched"`
	DrawingMatched bool              `json:"drawingMatched"`
	Matches        []Match           `json:"matches"`
	Metrics        analytics.Metrics `json:"metrics"`
	Unresolved     int               `json:"unresolved"`
	Warnings       []string          `json:"warnings,omitempty"`
}

// Evaluate compiles and runs a drawing over immutable prepared bars. It is a
// preview rather than a persisted experiment; the returned strategy JSON is
// the exact document to pass to the normal StudyService for a full run.
func Evaluate(ctx context.Context, d Drawing, series *core.Series, spec instrument.Spec, snapshot string) (Preview, error) {
	strategy, err := Compile(d)
	if err != nil {
		return Preview{}, err
	}
	if series == nil || series.Symbol != d.Symbol {
		return Preview{}, fmt.Errorf("setup test: prepared series does not match drawing symbol")
	}
	strategyJSON, err := json.Marshal(strategy)
	if err != nil {
		return Preview{}, fmt.Errorf("setup test: encode strategy: %w", err)
	}
	cfg := sim.Config{Spec: spec, Risk: sim.RiskModel{InitialEquity: 1_000_000, RiskPerTrade: core.RatioFromFloat(d.RiskPerTrade)}}
	result, _, err := engine.Run(ctx, strategy, engine.Data{Series: series, Config: cfg, Snapshot: snapshot})
	if err != nil {
		return Preview{}, err
	}
	returns := analytics.Returns{Pessimistic: make([]float64, len(result.Trades)), Optimistic: make([]float64, len(result.Trades))}
	out := Preview{Snapshot: snapshot, StrategyJSON: strategyJSON, StrategyHash: strategy.Hash(), Interpretation: ir.Describe(strategy).Summary, Unresolved: result.Unresolved, Matches: make([]Match, 0, len(result.Trades))}
	for i, trade := range result.Trades {
		signalBar := trade.EntryBar - 1
		if signalBar < 0 {
			signalBar = trade.EntryBar
		}
		match := Match{SignalTimeMS: series.Time[signalBar].Millis(), EntryTimeMS: trade.EntryTime.Millis(), Entry: trade.EntryPrice, Stop: trade.Stop, Target: trade.Target, Pessimistic: int64(trade.Pessimistic.Net), Optimistic: int64(trade.Optimistic.Net), Ambiguous: trade.Ambiguous}
		out.Matches = append(out.Matches, match)
		out.RuleMatched = out.RuleMatched || match.SignalTimeMS == d.AnchorTimeMS
		returns.Pessimistic[i], returns.Optimistic[i] = float64(trade.Pessimistic.Net), float64(trade.Optimistic.Net)
	}
	out.Metrics, err = analytics.MetricsFor(returns)
	if err != nil {
		return Preview{}, err
	}
	if len(out.Matches) < 10 {
		out.Warnings = append(out.Warnings, "Fewer than 10 historical matches; this is too little evidence for a strategy conclusion.")
	}
	out.DrawingMatched, err = drawingMatchesDetectedStructure(ctx, d, strategy, series, snapshot)
	if err != nil {
		return Preview{}, err
	}
	out.SampleMatched = out.RuleMatched && out.DrawingMatched
	if !out.RuleMatched {
		out.Warnings = append(out.Warnings, "The confirmed rule did not fire on the selected sample candle. Adjust the rule before relying on the backtest.")
	} else if !out.DrawingMatched {
		out.Warnings = append(out.Warnings, "The detected historical structure does not align with the level or zone you drew. Adjust the drawing or rule before relying on the backtest.")
	}
	return out, nil
}

func drawingMatchesDetectedStructure(ctx context.Context, d Drawing, strategy *ir.Strategy, series *core.Series, snapshot string) (bool, error) {
	if strategy == nil || len(strategy.Nodes) != 1 {
		return false, fmt.Errorf("setup test: expected one semantic detector node")
	}
	node := strategy.Nodes[0]
	detector, err := primitive.DefaultRegistry.Get(node.Pack, node.Type)
	if err != nil {
		return false, err
	}
	params := &structpb.Struct{}
	if err := protojson.Unmarshal(node.Params, params); err != nil {
		return false, fmt.Errorf("setup test: decode detector parameters: %w", err)
	}
	_, overlays, err := primitive.Evaluate(ctx, detector, params, node.Timeframe, series, snapshot, nil)
	if err != nil {
		return false, err
	}
	anchor := core.Time(d.AnchorTimeMS)
	tolerance := core.Price(d.ToleranceTicks)
	for _, overlay := range overlays {
		if anchor < overlay.From || anchor > overlay.To {
			continue
		}
		if d.Kind == BreakRetest && overlay.Kind == primitive.OverlayLevelLine {
			if overlay.Price >= d.LevelLow-tolerance && overlay.Price <= d.LevelHigh+tolerance {
				return true, nil
			}
		}
		if d.Kind != BreakRetest && overlay.Kind == primitive.OverlayZoneBox {
			if (d.LevelLow-overlay.Price).Abs() <= tolerance && (d.LevelHigh-overlay.Price2).Abs() <= tolerance {
				return true, nil
			}
		}
	}
	return false, nil
}

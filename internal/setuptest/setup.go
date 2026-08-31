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

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Bcypher01/Assay/internal/analytics"
	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/engine"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/ir"
	"github.com/Bcypher01/Assay/internal/packs/semantic"
	"github.com/Bcypher01/Assay/internal/primitive"
	"github.com/Bcypher01/Assay/internal/sim"
)

type Kind string

const (
	DrawnPrecedent     Kind = "drawn_precedent"
	BreakRetest        Kind = "break_retest"
	OrderBlockRetest   Kind = "order_block_retest"
	BreakerRetest      Kind = "breaker_retest"
	FairValueGapRetest Kind = "fair_value_gap_retest"
)

// PrecedentBar is one candle from the user-selected example, normalized to
// the example window's 0..1000 price range. It preserves shape while removing
// the absolute market price so the precedent can be compared across history
// and instruments.
type PrecedentBar struct {
	OpenMilli  int `json:"openMilli"`
	HighMilli  int `json:"highMilli"`
	LowMilli   int `json:"lowMilli"`
	CloseMilli int `json:"closeMilli"`
}

// Drawing is the complete user-confirmed meaning of one annotated setup. All
// price fields are instrument ticks at the API boundary.
type Drawing struct {
	Kind                  Kind           `json:"kind"`
	Side                  string         `json:"side"`
	Symbol                string         `json:"symbol"`
	Timeframe             string         `json:"timeframe"`
	AnchorTimeMS          int64          `json:"anchorTimeMs"`
	LevelLow              core.Price     `json:"levelLow"`
	LevelHigh             core.Price     `json:"levelHigh"`
	Entry                 core.Price     `json:"entry"`
	Stop                  core.Price     `json:"stop"`
	Target                core.Price     `json:"target"`
	LookbackBars          int            `json:"lookbackBars"`
	RetestBars            int            `json:"retestBars"`
	ToleranceTicks        int            `json:"toleranceTicks"`
	ExpiryBars            int            `json:"expiryBars"`
	DisplacementBodyMilli int            `json:"displacementBodyMilli"`
	UseBody               bool           `json:"useBody"`
	ConfirmByClose        bool           `json:"confirmByClose"`
	MinSizeTicks          int            `json:"minSizeTicks"`
	MitigationRule        string         `json:"mitigationRule"`
	RiskUnit              string         `json:"riskUnit"`
	RiskPerTrade          float64        `json:"riskPerTrade"`
	Precedent             []PrecedentBar `json:"precedent,omitempty"`
	SimilarityMilli       int            `json:"similarityMilli,omitempty"`
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
	case DrawnPrecedent, BreakRetest, OrderBlockRetest, BreakerRetest, FairValueGapRetest:
	default:
		add("kind", "must be a supported visual strategy pattern")
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
	switch {
	case d.Entry <= 0 || d.Stop <= 0 || d.Target <= 0:
		add("entry", "entry, stop, and target must be positive prices")
	case d.Side == "long" && !(d.Stop < d.Entry && d.Entry < d.Target):
		add("stop", "a long setup requires stop < entry < target")
	case d.Side == "short" && !(d.Target < d.Entry && d.Entry < d.Stop):
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
	if d.Kind == FairValueGapRetest {
		if d.MinSizeTicks < 1 {
			add("minSizeTicks", "must be at least 1 tick")
		}
		switch d.MitigationRule {
		case "first_touch", "half_fill", "full_fill":
		default:
			add("mitigationRule", "must be first_touch, half_fill, or full_fill")
		}
	}
	if d.Kind == DrawnPrecedent {
		if len(d.Precedent) < 3 || len(d.Precedent) > 64 {
			add("precedent", "draw an example spanning between 3 and 64 candles")
		}
		if d.SimilarityMilli < 500 || d.SimilarityMilli > 1000 {
			add("similarityMilli", "must be between 500 and 1000")
		}
		for _, bar := range d.Precedent {
			if bar.LowMilli < 0 || bar.HighMilli > 1000 || bar.LowMilli > bar.HighMilli || bar.OpenMilli < bar.LowMilli || bar.OpenMilli > bar.HighMilli || bar.CloseMilli < bar.LowMilli || bar.CloseMilli > bar.HighMilli {
				add("precedent", "contains an invalid normalized candle")
				break
			}
		}
	}
	if d.RiskUnit != "" && d.RiskUnit != "ticks" && d.RiskUnit != "percent" {
		add("riskUnit", "must be ticks or percent")
	}
	if d.RiskPerTrade <= 0 || d.RiskPerTrade > 0.1 || math.IsNaN(d.RiskPerTrade) || math.IsInf(d.RiskPerTrade, 0) {
		add("riskPerTrade", "must be greater than 0 and no more than 10%")
	}
	return out
}

type detectorParams struct {
	LookbackBars          int            `json:"lookbackBars"`
	RetestBars            int            `json:"retestBars"`
	ToleranceTicks        int            `json:"toleranceTicks"`
	ExpiryBars            int            `json:"expiryBars"`
	DisplacementBodyMilli int            `json:"displacementBodyMilli"`
	UseBody               bool           `json:"useBody"`
	ConfirmByClose        bool           `json:"confirmByClose"`
	MinSizeTicks          int            `json:"minSizeTicks"`
	MitigationRule        string         `json:"mitigationRule"`
	Precedent             []PrecedentBar `json:"precedent,omitempty"`
	SimilarityMilli       int            `json:"similarityMilli,omitempty"`
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
		MinSizeTicks: d.MinSizeTicks, MitigationRule: d.MitigationRule,
		Precedent: d.Precedent, SimilarityMilli: d.SimilarityMilli,
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
	stopDistance := core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(int64(stop))}
	targetDistance := core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(int64(target))}
	if d.RiskUnit == "percent" {
		stopDistance = core.Distance{Unit: core.UnitPercent, Value: core.RatioFromFloat(float64(stop) * 100 / float64(d.Entry))}
		targetDistance = core.Distance{Unit: core.UnitPercent, Value: core.RatioFromFloat(float64(target) * 100 / float64(d.Entry))}
	}
	return &ir.Strategy{
		Schema:     ir.SchemaV2,
		Name:       title(d),
		Universe:   ir.Universe{Symbols: []string{d.Symbol}},
		Timeframes: map[string]core.TF{"bias": tf, "poi": tf, "entry": tf},
		Risk: ir.Risk{
			Stop:     stopDistance,
			Target:   targetDistance,
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
	case FairValueGapRetest:
		return fmt.Sprintf("first %s %s of a fair value gap at least %d ticks wide", side, strings.ReplaceAll(d.MitigationRule, "_", " "), d.MinSizeTicks)
	case DrawnPrecedent:
		return fmt.Sprintf("%s price action matching the %d-candle drawn precedent at %d%% similarity", side, len(d.Precedent), d.SimilarityMilli/10)
	default:
		return fmt.Sprintf("first %s retest after an opposing candle zone becomes a breaker", side)
	}
}

type Match struct {
	SignalTimeMS          int64      `json:"signalTimeMs"`
	EntryTimeMS           int64      `json:"entryTimeMs"`
	ExitTimeMSPessimistic int64      `json:"exitTimeMsPessimistic"`
	ExitTimeMSOptimistic  int64      `json:"exitTimeMsOptimistic"`
	Entry                 core.Price `json:"entry"`
	ExitPessimistic       core.Price `json:"exitPessimistic"`
	ExitOptimistic        core.Price `json:"exitOptimistic"`
	Stop                  core.Price `json:"stop"`
	Target                core.Price `json:"target"`
	LevelLow              core.Price `json:"levelLow"`
	LevelHigh             core.Price `json:"levelHigh"`
	Pessimistic           int64      `json:"pessimisticPnl"`
	Optimistic            int64      `json:"optimisticPnl"`
	Ambiguous             bool       `json:"ambiguous"`
	SimilarityMilli       int        `json:"similarityMilli,omitempty"`
	ShapeMilli            int        `json:"shapeMilli,omitempty"`
	StructureMilli        int        `json:"structureMilli,omitempty"`
	DirectionMilli        int        `json:"directionMilli,omitempty"`
	AnatomyMilli          int        `json:"anatomyMilli,omitempty"`
	TriggerMilli          int        `json:"triggerMilli,omitempty"`
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
	returns := analytics.Returns{Pessimistic: make([]float64, 0, len(result.Trades)), Optimistic: make([]float64, 0, len(result.Trades))}
	out := Preview{Snapshot: snapshot, StrategyJSON: strategyJSON, StrategyHash: strategy.Hash(), Interpretation: ir.Describe(strategy).Summary, Matches: make([]Match, 0, len(result.Trades))}
	anchorBar := seriesIndexAtTime(series, d.AnchorTimeMS)
	precedent := semanticPrecedent(d.Precedent)
	for _, trade := range result.Trades {
		signalBar := trade.EntryBar - 1
		if signalBar < 0 {
			signalBar = trade.EntryBar
		}
		// The user's source window proves only that the detector can recognize
		// its input. It is not independent historical evidence and must not be
		// counted as a discovered occurrence or included in performance metrics.
		if d.Kind == DrawnPrecedent && windowsOverlap(signalBar, anchorBar, len(d.Precedent)) {
			continue
		}
		match := Match{
			SignalTimeMS:          series.Time[signalBar].Millis(),
			EntryTimeMS:           trade.EntryTime.Millis(),
			ExitTimeMSPessimistic: trade.Pessimistic.ExitTime.Millis(),
			ExitTimeMSOptimistic:  trade.Optimistic.ExitTime.Millis(),
			Entry:                 trade.EntryPrice,
			ExitPessimistic:       trade.Pessimistic.ExitPrice,
			ExitOptimistic:        trade.Optimistic.ExitPrice,
			Stop:                  trade.Stop,
			Target:                trade.Target,
			Pessimistic:           int64(trade.Pessimistic.Net),
			Optimistic:            int64(trade.Optimistic.Net),
			Ambiguous:             trade.Ambiguous,
		}
		if d.Kind == FairValueGapRetest {
			match.LevelLow, match.LevelHigh = fairValueGapAtSignal(d, series, signalBar)
		} else if d.Kind == DrawnPrecedent {
			match.LevelLow, match.LevelHigh = precedentRangeAtSignal(d, series, signalBar)
			score := semantic.ScoreSeriesPrecedent(precedent, series, signalBar, d.SimilarityMilli)
			match.SimilarityMilli = score.Overall
			match.ShapeMilli = score.Shape
			match.StructureMilli = score.Structure
			match.DirectionMilli = score.Direction
			match.AnatomyMilli = score.Anatomy
			match.TriggerMilli = score.Trigger
		}
		out.Matches = append(out.Matches, match)
		out.RuleMatched = out.RuleMatched || match.SignalTimeMS == d.AnchorTimeMS
		returns.Pessimistic = append(returns.Pessimistic, float64(trade.Pessimistic.Net))
		returns.Optimistic = append(returns.Optimistic, float64(trade.Optimistic.Net))
		if trade.Ambiguous {
			out.Unresolved++
		}
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
	if d.Kind == DrawnPrecedent {
		// Dense precedent matches can overlap an already-open simulated trade.
		// The detector signal, rather than whether the simulator opened another
		// position on that bar, is the correct sample-validation criterion.
		out.RuleMatched = out.DrawingMatched
		out.Warnings = append(out.Warnings, "The source example and overlapping candles are excluded from occurrence counts and performance figures.")
	}
	out.SampleMatched = out.RuleMatched && out.DrawingMatched
	if !out.RuleMatched {
		out.Warnings = append(out.Warnings, "The confirmed rule did not fire on the selected sample candle. Adjust the rule before relying on the backtest.")
	} else if !out.DrawingMatched {
		out.Warnings = append(out.Warnings, "The detected historical structure does not align with the level or zone you drew. Adjust the drawing or rule before relying on the backtest.")
	}
	return out, nil
}

func semanticPrecedent(bars []PrecedentBar) []semantic.PrecedentCandle {
	out := make([]semantic.PrecedentCandle, len(bars))
	for i, bar := range bars {
		out[i] = semantic.PrecedentCandle{Open: bar.OpenMilli, High: bar.HighMilli, Low: bar.LowMilli, Close: bar.CloseMilli}
	}
	return out
}

func seriesIndexAtTime(series *core.Series, timeMS int64) int {
	if series == nil {
		return -1
	}
	for i, at := range series.Time {
		if at.Millis() == timeMS {
			return i
		}
	}
	return -1
}

func windowsOverlap(leftSignal, rightSignal, length int) bool {
	if leftSignal < 0 || rightSignal < 0 || length < 1 {
		return false
	}
	leftFrom, rightFrom := leftSignal-length+1, rightSignal-length+1
	return leftFrom <= rightSignal && rightFrom <= leftSignal
}

func precedentRangeAtSignal(d Drawing, series *core.Series, signalBar int) (core.Price, core.Price) {
	from := signalBar - len(d.Precedent) + 1
	if from < 0 || signalBar >= series.Len() {
		return 0, 0
	}
	low, high := series.Low[from], series.High[from]
	for i := from + 1; i <= signalBar; i++ {
		low = core.Min(low, series.Low[i])
		high = core.Max(high, series.High[i])
	}
	return low, high
}

func fairValueGapAtSignal(d Drawing, series *core.Series, signalBar int) (core.Price, core.Price) {
	type gap struct {
		low, high        core.Price
		created, expires int
	}
	var pending *gap
	filled := func(i int, z *gap) bool {
		mid := z.low + (z.high-z.low)/2
		switch d.MitigationRule {
		case "first_touch":
			return series.Low[i] <= z.high && series.High[i] >= z.low
		case "half_fill":
			if d.Side == "long" {
				return series.Low[i] <= mid
			}
			return series.High[i] >= mid
		case "full_fill":
			if d.Side == "long" {
				return series.Low[i] <= z.low
			}
			return series.High[i] >= z.high
		default:
			return false
		}
	}
	for i := 0; i <= signalBar && i < series.Len(); i++ {
		if pending != nil {
			if i > pending.expires {
				pending = nil
			} else if i > pending.created && filled(i, pending) {
				if i == signalBar {
					return pending.low, pending.high
				}
				pending = nil
			}
		}
		if i < 2 {
			continue
		}
		var low, high core.Price
		if d.Side == "long" {
			low, high = series.High[i-2], series.Low[i]
		} else {
			low, high = series.High[i], series.Low[i-2]
		}
		if high-low >= core.Price(d.MinSizeTicks) {
			pending = &gap{low: low, high: high, created: i, expires: min(i+d.ExpiryBars, series.Len()-1)}
		}
	}
	return 0, 0
}

func drawingMatchesDetectedStructure(ctx context.Context, d Drawing, strategy *ir.Strategy, series *core.Series, snapshot string) (bool, error) {
	if d.Kind == DrawnPrecedent {
		return drawnPrecedentMatchesAtAnchor(d, series), nil
	}
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
		if d.Kind == DrawnPrecedent && overlay.Kind == primitive.OverlayMarker && overlay.From == anchor {
			return true, nil
		}
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

func drawnPrecedentMatchesAtAnchor(d Drawing, series *core.Series) bool {
	if series == nil || len(d.Precedent) < 3 {
		return false
	}
	anchor := -1
	for i, at := range series.Time {
		if at.Millis() == d.AnchorTimeMS {
			anchor = i
			break
		}
	}
	from := anchor - len(d.Precedent) + 1
	if anchor < 0 || from < 0 {
		return false
	}
	low, high := series.Low[from], series.High[from]
	for i := from + 1; i <= anchor; i++ {
		low = core.Min(low, series.Low[i])
		high = core.Max(high, series.High[i])
	}
	span := high - low
	if span <= 0 {
		return false
	}
	errorSum := 0
	abs := func(value int) int {
		if value < 0 {
			return -value
		}
		return value
	}
	for i, expected := range d.Precedent {
		at := from + i
		normalize := func(price core.Price) int {
			return int(((price-low)*1000 + span/2) / span)
		}
		errorSum += abs(normalize(series.Open[at]) - expected.OpenMilli)
		errorSum += abs(normalize(series.High[at]) - expected.HighMilli)
		errorSum += abs(normalize(series.Low[at]) - expected.LowMilli)
		errorSum += abs(normalize(series.Close[at]) - expected.CloseMilli)
	}
	return errorSum/(len(d.Precedent)*4) <= 1000-d.SimilarityMilli
}

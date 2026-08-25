package ir

import (
	"encoding/json"

	"github.com/Bcypher01/Assay/internal/core"
)

const (
	MovingAverageCrossoverTemplateID      = "moving-average-crossover"
	MovingAverageCrossoverTemplateVersion = 1
	RSIPullbackTemplateID                 = "rsi-pullback-in-trend"
	RSIPullbackTemplateVersion            = 1
	DonchianBreakoutTemplateID            = "donchian-breakout"
	DonchianBreakoutTemplateVersion       = 1
	BollingerReversionTemplateID          = "bollinger-mean-reversion"
	BollingerReversionTemplateVersion     = 1
)

// NewMovingAverageCrossover returns Assay's first editable stock template.
// Defaults are examples, not trading advice: EUR/USD daily data, a 20/50 SMA
// crossover, 0.5% risk per trade, a 50-pip stop, and a 2R target.
func NewMovingAverageCrossover() *Strategy {
	return &Strategy{
		Schema:   SchemaV2,
		Name:     "20/50 moving-average crossover",
		Universe: Universe{Symbols: []string{"EUR/USD"}},
		Timeframes: map[string]core.TF{
			"bias": core.TFD1, "poi": core.TFD1, "entry": core.TFD1,
		},
		Risk: Risk{
			Stop:     core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(50)},
			Target:   core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(100)},
			PerTrade: core.RatioFromFloat(0.005),
		},
		Template: Template{ID: MovingAverageCrossoverTemplateID, Version: MovingAverageCrossoverTemplateVersion},
		Entries: Entries{
			Timing: EntryTimingNextOpen,
			Long: EntryBranch{
				Enabled: true,
				Name:    "Bullish 20/50 crossover",
				When:    Expression{Node: "bullish-cross"},
			},
			Short: EntryBranch{
				Enabled: true,
				Name:    "Bearish 20/50 crossover",
				When:    Expression{Node: "bearish-cross"},
			},
		},
		Nodes: []Node{
			{
				ID: "bullish-cross", Pack: "indicator", Type: "ma_cross_bullish",
				Params: crossoverParams(), Timeframe: core.TFD1,
			},
			{
				ID: "bearish-cross", Pack: "indicator", Type: "ma_cross_bearish",
				Params: crossoverParams(), Timeframe: core.TFD1,
			},
		},
	}
}

func crossoverParams() json.RawMessage {
	return json.RawMessage(`{"fastPeriod":20,"slowPeriod":50}`)
}

// NewRSIPullback returns an editable trend-pullback template. A long requires
// price above its 50-period SMA and RSI to cross back above 30; the short rule
// is the symmetric price-below-SMA and RSI-crosses-below-70 condition.
func NewRSIPullback() *Strategy {
	return &Strategy{
		Schema:   SchemaV2,
		Name:     "RSI pullback in trend",
		Universe: Universe{Symbols: []string{"EUR/USD"}},
		Timeframes: map[string]core.TF{
			"bias": core.TFD1, "poi": core.TFD1, "entry": core.TFD1,
		},
		Risk: Risk{
			Stop:     core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(50)},
			Target:   core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(100)},
			PerTrade: core.RatioFromFloat(0.005),
		},
		Template: Template{ID: RSIPullbackTemplateID, Version: RSIPullbackTemplateVersion},
		Entries: Entries{
			Timing: EntryTimingNextOpen,
			Long: EntryBranch{Enabled: true, Name: "RSI crosses above 30 in an uptrend", When: Expression{All: []Expression{
				{Price: &PriceLevelPredicate{Node: "trend", Source: PriceClose, Relation: PriceAbove}},
				{Comparison: &NumberComparison{Node: "rsi", Operator: ComparisonCrossesAbove, Threshold: core.RatioFromInt(30)}},
			}}},
			Short: EntryBranch{Enabled: true, Name: "RSI crosses below 70 in a downtrend", When: Expression{All: []Expression{
				{Price: &PriceLevelPredicate{Node: "trend", Source: PriceClose, Relation: PriceBelow}},
				{Comparison: &NumberComparison{Node: "rsi", Operator: ComparisonCrossesBelow, Threshold: core.RatioFromInt(70)}},
			}}},
		},
		Nodes: []Node{
			{ID: "trend", Pack: "indicator", Type: "sma", Params: json.RawMessage(`{"period":50}`), Timeframe: core.TFD1},
			{ID: "rsi", Pack: "indicator", Type: "rsi", Params: json.RawMessage(`{"period":14}`), Timeframe: core.TFD1},
		},
	}
}

// NewDonchianBreakout returns an editable channel-breakout template. Its
// boundaries are calculated from the preceding 20 bars, so the breakout close
// is compared against a level it did not itself alter.
func NewDonchianBreakout() *Strategy {
	return &Strategy{
		Schema:   SchemaV2,
		Name:     "20-bar Donchian breakout",
		Universe: Universe{Symbols: []string{"EUR/USD"}},
		Timeframes: map[string]core.TF{
			"bias": core.TFD1, "poi": core.TFD1, "entry": core.TFD1,
		},
		Risk: Risk{
			Stop:     core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(50)},
			Target:   core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(100)},
			PerTrade: core.RatioFromFloat(0.005),
		},
		Template: Template{ID: DonchianBreakoutTemplateID, Version: DonchianBreakoutTemplateVersion},
		Entries: Entries{
			Timing: EntryTimingNextOpen,
			Long: EntryBranch{Enabled: true, Name: "Close crosses above the 20-bar high", When: Expression{
				Price: &PriceLevelPredicate{Node: "channel-high", Source: PriceClose, Relation: PriceCrossesAbove},
			}},
			Short: EntryBranch{Enabled: true, Name: "Close crosses below the 20-bar low", When: Expression{
				Price: &PriceLevelPredicate{Node: "channel-low", Source: PriceClose, Relation: PriceCrossesBelow},
			}},
		},
		Nodes: []Node{
			{ID: "channel-high", Pack: "indicator", Type: "donchian_high", Params: json.RawMessage(`{"period":20}`), Timeframe: core.TFD1},
			{ID: "channel-low", Pack: "indicator", Type: "donchian_low", Params: json.RawMessage(`{"period":20}`), Timeframe: core.TFD1},
		},
	}
}

// NewBollingerReversion returns an editable mean-reversion template. A long
// first reaches below the lower band and then closes back above it; the short
// branch mirrors the sequence at the upper band.
func NewBollingerReversion() *Strategy {
	return &Strategy{
		Schema:   SchemaV2,
		Name:     "Bollinger-band mean reversion",
		Universe: Universe{Symbols: []string{"EUR/USD"}},
		Timeframes: map[string]core.TF{
			"bias": core.TFD1, "poi": core.TFD1, "entry": core.TFD1,
		},
		Risk: Risk{
			Stop:     core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(50)},
			Target:   core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(100)},
			PerTrade: core.RatioFromFloat(0.005),
		},
		Template: Template{ID: BollingerReversionTemplateID, Version: BollingerReversionTemplateVersion},
		Entries: Entries{
			Timing: EntryTimingNextOpen,
			Long: EntryBranch{Enabled: true, Name: "Low reaches below then close recovers above the lower band", When: Expression{All: []Expression{
				{Price: &PriceLevelPredicate{Node: "lower-band", Source: PriceLow, Relation: PriceBelow}},
				{Price: &PriceLevelPredicate{Node: "lower-band", Source: PriceClose, Relation: PriceCrossesAbove}},
			}}},
			Short: EntryBranch{Enabled: true, Name: "High reaches above then close falls below the upper band", When: Expression{All: []Expression{
				{Price: &PriceLevelPredicate{Node: "upper-band", Source: PriceHigh, Relation: PriceAbove}},
				{Price: &PriceLevelPredicate{Node: "upper-band", Source: PriceClose, Relation: PriceCrossesBelow}},
			}}},
		},
		Nodes: []Node{
			{ID: "upper-band", Pack: "indicator", Type: "bollinger_upper", Params: bollingerParams(), Timeframe: core.TFD1},
			{ID: "lower-band", Pack: "indicator", Type: "bollinger_lower", Params: bollingerParams(), Timeframe: core.TFD1},
		},
	}
}

func bollingerParams() json.RawMessage {
	return json.RawMessage(`{"period":20,"widthMilli":2000}`)
}

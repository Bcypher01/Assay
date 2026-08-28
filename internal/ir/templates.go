package ir

import (
	"encoding/json"

	"github.com/Bcypher01/Assay/internal/core"
)

// exampleSymbol is deliberately the catalogue identity rather than its display
// label. A template is executable input, so using "EUR/USD" here would leave a
// new installation unable to resolve its instrument or prepare its data.
const exampleSymbol = "DUKAS:EURUSD"
const bitcoinSymbol = "BINANCE:BTCUSDT"
const ethereumSymbol = "BINANCE:ETHUSDT"

const (
	MovingAverageCrossoverTemplateID       = "moving-average-crossover"
	MovingAverageCrossoverTemplateVersion  = 1
	RSIPullbackTemplateID                  = "rsi-pullback-in-trend"
	RSIPullbackTemplateVersion             = 1
	DonchianBreakoutTemplateID             = "donchian-breakout"
	DonchianBreakoutTemplateVersion        = 1
	BollingerReversionTemplateID           = "bollinger-mean-reversion"
	BollingerReversionTemplateVersion      = 1
	SessionBreakoutTemplateID              = "london-session-breakout"
	SessionBreakoutTemplateVersion         = 1
	LiquiditySweepStructureTemplateID      = "liquidity-sweep-structure-confirmation"
	LiquiditySweepStructureTemplateVersion = 1
	BitcoinPullbackTemplateID              = "bitcoin-pullback-20sma"
	BitcoinPullbackTemplateVersion         = 1
	EthereumBreakoutTemplateID             = "ethereum-range-breakout"
	EthereumBreakoutTemplateVersion        = 1
	BitcoinSweepShiftTemplateID            = "bitcoin-sweep-structure-shift"
	BitcoinSweepShiftTemplateVersion       = 1
)

// TemplateDefinition is stable catalogue metadata for a strategy that can be
// expressed and executed by the engine. It is deliberately separate from UI
// presentation assets such as charts or icons.
type TemplateDefinition struct {
	ID                string
	Version           uint32
	Name              string
	Family            string
	Description       string
	ExpectedFrequency string
	New               func() *Strategy
}

// PopularTemplates returns fresh, editable documents in a stable catalogue
// order. Callers must not mutate a document and reuse it for another request.
func PopularTemplates() []TemplateDefinition {
	return []TemplateDefinition{
		{MovingAverageCrossoverTemplateID, MovingAverageCrossoverTemplateVersion, "20/50 moving-average crossover", "Trend following", "Captures sustained directional moves and gives most of them back at the turn.", "Low", NewMovingAverageCrossover},
		{RSIPullbackTemplateID, RSIPullbackTemplateVersion, "RSI pullback in a trend", "Mean reversion + trend", "Buys temporary weakness while the larger trend is still intact.", "Medium", NewRSIPullback},
		{DonchianBreakoutTemplateID, DonchianBreakoutTemplateVersion, "20-bar Donchian breakout", "Trend following", "Enters when price leaves an established range, looking for continuation.", "Low", NewDonchianBreakout},
		{BollingerReversionTemplateID, BollingerReversionTemplateVersion, "Bollinger-band mean reversion", "Mean reversion", "Looks for price to return inside a stretched Bollinger band.", "High", NewBollingerReversion},
		{SessionBreakoutTemplateID, SessionBreakoutTemplateVersion, "London session breakout", "Price action", "Trades a break of a completed session range using an explicit UTC window.", "Medium", NewSessionBreakout},
		{LiquiditySweepStructureTemplateID, LiquiditySweepStructureTemplateVersion, "Liquidity sweep with market-structure confirmation", "SMC workflow", "Waits for a sweep and a later directional structure confirmation.", "Low", NewLiquiditySweepStructure},
		{BitcoinPullbackTemplateID, BitcoinPullbackTemplateVersion, "Bitcoin pullback to the 20-period average", "Crypto trend following", "Waits for BTC to pull back to its 20-period average before entering in the trend direction.", "Medium", NewBitcoinPullback},
		{EthereumBreakoutTemplateID, EthereumBreakoutTemplateVersion, "Ethereum range compression breakout", "Crypto price action", "Enters when ETH leaves a tight range with a decisive close.", "Medium", NewEthereumRangeBreakout},
		{BitcoinSweepShiftTemplateID, BitcoinSweepShiftTemplateVersion, "Bitcoin liquidity sweep, then structure shift", "Crypto SMC workflow", "Waits for BTC to sweep a prior extreme and confirm a directional structure shift.", "Low", NewBitcoinSweepShift},
	}
}

// NewMovingAverageCrossover returns Assay's first editable stock template.
// Defaults are examples, not trading advice: EUR/USD daily data, a 20/50 SMA
// crossover, 0.5% risk per trade, a 50-pip stop, and a 2R target.
func NewMovingAverageCrossover() *Strategy {
	return &Strategy{
		Schema:   SchemaV2,
		Name:     "20/50 moving-average crossover",
		Universe: Universe{Symbols: []string{exampleSymbol}},
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
		Universe: Universe{Symbols: []string{exampleSymbol}},
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
		Universe: Universe{Symbols: []string{exampleSymbol}},
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
		Universe: Universe{Symbols: []string{exampleSymbol}},
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

// NewSessionBreakout returns an editable UTC session-range breakout template.
// The 07:00–09:00 UTC range is an example configuration, not a claim about an
// exchange session or daylight-saving schedule; users must set their market's
// intended UTC window explicitly.
func NewSessionBreakout() *Strategy {
	return &Strategy{
		Schema:   SchemaV2,
		Name:     "London session breakout (UTC example)",
		Universe: Universe{Symbols: []string{exampleSymbol}},
		Timeframes: map[string]core.TF{
			"bias": core.TFM15, "poi": core.TFM15, "entry": core.TFM15,
		},
		Risk: Risk{
			Stop:     core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(25)},
			Target:   core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(50)},
			PerTrade: core.RatioFromFloat(0.005),
		},
		Template: Template{ID: SessionBreakoutTemplateID, Version: SessionBreakoutTemplateVersion},
		Entries: Entries{
			Timing: EntryTimingNextOpen,
			Long: EntryBranch{Enabled: true, Name: "Close crosses above the completed session high", When: Expression{
				Price: &PriceLevelPredicate{Node: "session-high", Source: PriceClose, Relation: PriceCrossesAbove},
			}},
			Short: EntryBranch{Enabled: true, Name: "Close crosses below the completed session low", When: Expression{
				Price: &PriceLevelPredicate{Node: "session-low", Source: PriceClose, Relation: PriceCrossesBelow},
			}},
		},
		Nodes: []Node{
			{ID: "session-high", Pack: "timeofday", Type: "session_high", Params: sessionParams(), Timeframe: core.TFM15},
			{ID: "session-low", Pack: "timeofday", Type: "session_low", Params: sessionParams(), Timeframe: core.TFM15},
		},
	}
}

// NewLiquiditySweepStructure returns an editable reversal template. A sweep
// must be followed within five bars by a directional break of market structure,
// preventing the template from treating the sweep itself as an entry signal.
func NewLiquiditySweepStructure() *Strategy {
	return &Strategy{
		Schema:   SchemaV2,
		Name:     "Liquidity sweep with market-structure confirmation",
		Universe: Universe{Symbols: []string{exampleSymbol}},
		Timeframes: map[string]core.TF{
			"bias": core.TFH1, "poi": core.TFH1, "entry": core.TFH1,
		},
		Risk: Risk{
			Stop:     core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(30)},
			Target:   core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(60)},
			PerTrade: core.RatioFromFloat(0.005),
		},
		Template: Template{ID: LiquiditySweepStructureTemplateID, Version: LiquiditySweepStructureTemplateVersion},
		Entries: Entries{
			Timing: EntryTimingNextOpen,
			Long: EntryBranch{Enabled: true, Name: "Bullish sweep followed by bullish structure", When: Expression{After: &OccursAfter{
				Event: Expression{Node: "bullish-structure"}, Predecessor: Expression{Node: "bullish-sweep"}, WithinBars: 5,
			}}},
			Short: EntryBranch{Enabled: true, Name: "Bearish sweep followed by bearish structure", When: Expression{After: &OccursAfter{
				Event: Expression{Node: "bearish-structure"}, Predecessor: Expression{Node: "bearish-sweep"}, WithinBars: 5,
			}}},
		},
		Nodes: []Node{
			{ID: "bullish-sweep", Pack: "smc", Type: "liquidity_sweep_bullish", Params: sweepParams(), Timeframe: core.TFH1},
			{ID: "bearish-sweep", Pack: "smc", Type: "liquidity_sweep_bearish", Params: sweepParams(), Timeframe: core.TFH1},
			{ID: "bullish-structure", Pack: "smc", Type: "market_structure_bullish", Params: structureParams(), Timeframe: core.TFH1},
			{ID: "bearish-structure", Pack: "smc", Type: "market_structure_bearish", Params: structureParams(), Timeframe: core.TFH1},
		},
	}
}

func cryptoRisk(stop, target float64) Risk {
	return Risk{
		Stop:     core.Distance{Unit: core.UnitPercent, Value: core.RatioFromFloat(stop)},
		Target:   core.Distance{Unit: core.UnitPercent, Value: core.RatioFromFloat(target)},
		PerTrade: core.RatioFromFloat(0.005),
	}
}

// NewBitcoinPullback uses percentage exits because BTC's price scale makes FX
// pip distances misleading. It retains the same two-sided structure as the
// rest of the catalogue so both directional conditions are explicit.
func NewBitcoinPullback() *Strategy {
	return &Strategy{
		Schema: SchemaV2, Name: "Bitcoin pullback to the 20-period average",
		Universe:   Universe{Symbols: []string{bitcoinSymbol}},
		Timeframes: map[string]core.TF{"bias": core.TFH4, "poi": core.TFH4, "entry": core.TFH4},
		Risk:       cryptoRisk(1.5, 3.0),
		Template:   Template{ID: BitcoinPullbackTemplateID, Version: BitcoinPullbackTemplateVersion},
		Entries: Entries{Timing: EntryTimingNextOpen,
			Long: EntryBranch{Enabled: true, Name: "Bullish 20-period average pullback", When: Expression{All: []Expression{
				{Price: &PriceLevelPredicate{Node: "trend", Source: PriceClose, Relation: PriceAbove}},
				{Price: &PriceLevelPredicate{Node: "average", Source: PriceClose, Relation: PriceCrossesAbove}},
			}}},
			Short: EntryBranch{Enabled: true, Name: "Bearish 20-period average pullback", When: Expression{All: []Expression{
				{Price: &PriceLevelPredicate{Node: "trend", Source: PriceClose, Relation: PriceBelow}},
				{Price: &PriceLevelPredicate{Node: "average", Source: PriceClose, Relation: PriceCrossesBelow}},
			}}},
		},
		Nodes: []Node{
			{ID: "trend", Pack: "indicator", Type: "sma", Params: json.RawMessage(`{"period":50}`), Timeframe: core.TFH4},
			{ID: "average", Pack: "indicator", Type: "sma", Params: json.RawMessage(`{"period":20}`), Timeframe: core.TFH4},
		},
	}
}

func NewEthereumRangeBreakout() *Strategy {
	return &Strategy{
		Schema: SchemaV2, Name: "Ethereum range compression breakout",
		Universe:   Universe{Symbols: []string{ethereumSymbol}},
		Timeframes: map[string]core.TF{"bias": core.TFH1, "poi": core.TFH1, "entry": core.TFH1},
		Risk:       cryptoRisk(1.2, 2.4),
		Template:   Template{ID: EthereumBreakoutTemplateID, Version: EthereumBreakoutTemplateVersion},
		Entries: Entries{Timing: EntryTimingNextOpen,
			Long:  EntryBranch{Enabled: true, Name: "Close crosses above the 20-bar high", When: Expression{Price: &PriceLevelPredicate{Node: "channel-high", Source: PriceClose, Relation: PriceCrossesAbove}}},
			Short: EntryBranch{Enabled: true, Name: "Close crosses below the 20-bar low", When: Expression{Price: &PriceLevelPredicate{Node: "channel-low", Source: PriceClose, Relation: PriceCrossesBelow}}},
		},
		Nodes: []Node{
			{ID: "channel-high", Pack: "indicator", Type: "donchian_high", Params: json.RawMessage(`{"period":20}`), Timeframe: core.TFH1},
			{ID: "channel-low", Pack: "indicator", Type: "donchian_low", Params: json.RawMessage(`{"period":20}`), Timeframe: core.TFH1},
		},
	}
}

func NewBitcoinSweepShift() *Strategy {
	return &Strategy{
		Schema: SchemaV2, Name: "Bitcoin liquidity sweep, then structure shift",
		Universe:   Universe{Symbols: []string{bitcoinSymbol}},
		Timeframes: map[string]core.TF{"bias": core.TFH1, "poi": core.TFH1, "entry": core.TFH1},
		Risk:       cryptoRisk(1.0, 2.5),
		Template:   Template{ID: BitcoinSweepShiftTemplateID, Version: BitcoinSweepShiftTemplateVersion},
		Entries: Entries{Timing: EntryTimingNextOpen,
			Long:  EntryBranch{Enabled: true, Name: "Bullish sweep followed by bullish structure", When: Expression{After: &OccursAfter{Event: Expression{Node: "bullish-structure"}, Predecessor: Expression{Node: "bullish-sweep"}, WithinBars: 5}}},
			Short: EntryBranch{Enabled: true, Name: "Bearish sweep followed by bearish structure", When: Expression{After: &OccursAfter{Event: Expression{Node: "bearish-structure"}, Predecessor: Expression{Node: "bearish-sweep"}, WithinBars: 5}}},
		},
		Nodes: []Node{
			{ID: "bullish-sweep", Pack: "smc", Type: "liquidity_sweep_bullish", Params: sweepParams(), Timeframe: core.TFH1},
			{ID: "bearish-sweep", Pack: "smc", Type: "liquidity_sweep_bearish", Params: sweepParams(), Timeframe: core.TFH1},
			{ID: "bullish-structure", Pack: "smc", Type: "market_structure_bullish", Params: structureParams(), Timeframe: core.TFH1},
			{ID: "bearish-structure", Pack: "smc", Type: "market_structure_bearish", Params: structureParams(), Timeframe: core.TFH1},
		},
	}
}

func sessionParams() json.RawMessage {
	return json.RawMessage(`{"startMinuteUtc":420,"endMinuteUtc":540}`)
}

func bollingerParams() json.RawMessage {
	return json.RawMessage(`{"period":20,"widthMilli":2000}`)
}

func sweepParams() json.RawMessage {
	return json.RawMessage(`{"wickThroughTicks":1,"closeBackBars":3,"requireStructureShift":true}`)
}

func structureParams() json.RawMessage {
	return json.RawMessage(`{"swingLookback":5,"confirmByClose":true}`)
}

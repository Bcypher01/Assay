package ir

import (
	"testing"

	"github.com/bcypher01/assay/internal/core"
	_ "github.com/bcypher01/assay/internal/packs/indicator"
	_ "github.com/bcypher01/assay/internal/packs/smc"
	_ "github.com/bcypher01/assay/internal/packs/timeofday"
	"github.com/bcypher01/assay/internal/primitive"
)

func TestNewMovingAverageCrossover(t *testing.T) {
	strategy := NewMovingAverageCrossover()
	if errs := strategy.Validate(primitive.DefaultRegistry); len(errs) != 0 {
		t.Fatalf("template is invalid: %#v", errs)
	}
	if strategy.Schema != SchemaV2 || strategy.Template.ID != MovingAverageCrossoverTemplateID || strategy.Template.Version != MovingAverageCrossoverTemplateVersion {
		t.Fatalf("template identity = %#v", strategy)
	}
	if strategy.Universe.Symbols[0] != exampleSymbol || strategy.Timeframes["entry"] != core.TFD1 {
		t.Fatalf("unexpected default market/timeframe: %#v", strategy)
	}
	if strategy.Risk.Stop != (core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(50)}) ||
		strategy.Risk.Target != (core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(100)}) ||
		strategy.Risk.PerTrade != core.RatioFromFloat(0.005) {
		t.Fatalf("unexpected risk defaults: %#v", strategy.Risk)
	}
	if len(strategy.Nodes) != 2 || strategy.Nodes[0].Type != "ma_cross_bullish" || strategy.Nodes[1].Type != "ma_cross_bearish" {
		t.Fatalf("unexpected directional nodes: %#v", strategy.Nodes)
	}
	if strategy.Entries.Long.When.Node != "bullish-cross" || strategy.Entries.Short.When.Node != "bearish-cross" {
		t.Fatalf("unexpected branch rules: %#v", strategy.Entries)
	}
}

func TestNewRSIPullback(t *testing.T) {
	strategy := NewRSIPullback()
	if errs := strategy.Validate(primitive.DefaultRegistry); len(errs) != 0 {
		t.Fatalf("template is invalid: %#v", errs)
	}
	if strategy.Template.ID != RSIPullbackTemplateID || strategy.Template.Version != RSIPullbackTemplateVersion {
		t.Fatalf("template identity = %#v", strategy.Template)
	}
	if strategy.Entries.Long.When.All[0].Price.Relation != PriceAbove ||
		strategy.Entries.Long.When.All[1].Comparison.Operator != ComparisonCrossesAbove ||
		strategy.Entries.Long.When.All[1].Comparison.Threshold != core.RatioFromInt(30) {
		t.Fatalf("unexpected long branch: %#v", strategy.Entries.Long)
	}
	if strategy.Entries.Short.When.All[0].Price.Relation != PriceBelow ||
		strategy.Entries.Short.When.All[1].Comparison.Operator != ComparisonCrossesBelow ||
		strategy.Entries.Short.When.All[1].Comparison.Threshold != core.RatioFromInt(70) {
		t.Fatalf("unexpected short branch: %#v", strategy.Entries.Short)
	}
}

func TestNewDonchianBreakout(t *testing.T) {
	strategy := NewDonchianBreakout()
	if errs := strategy.Validate(primitive.DefaultRegistry); len(errs) != 0 {
		t.Fatalf("template is invalid: %#v", errs)
	}
	if strategy.Template.ID != DonchianBreakoutTemplateID || strategy.Template.Version != DonchianBreakoutTemplateVersion {
		t.Fatalf("template identity = %#v", strategy.Template)
	}
	if strategy.Entries.Long.When.Price.Relation != PriceCrossesAbove || strategy.Entries.Short.When.Price.Relation != PriceCrossesBelow {
		t.Fatalf("unexpected breakout branches: %#v", strategy.Entries)
	}
	if len(strategy.Nodes) != 2 || strategy.Nodes[0].Type != "donchian_high" || strategy.Nodes[1].Type != "donchian_low" {
		t.Fatalf("unexpected channel nodes: %#v", strategy.Nodes)
	}
}

func TestNewBollingerReversion(t *testing.T) {
	strategy := NewBollingerReversion()
	if errs := strategy.Validate(primitive.DefaultRegistry); len(errs) != 0 {
		t.Fatalf("template is invalid: %#v", errs)
	}
	if strategy.Template.ID != BollingerReversionTemplateID || strategy.Template.Version != BollingerReversionTemplateVersion {
		t.Fatalf("template identity = %#v", strategy.Template)
	}
	if strategy.Entries.Long.When.All[0].Price.Source != PriceLow || strategy.Entries.Long.When.All[1].Price.Relation != PriceCrossesAbove {
		t.Fatalf("unexpected long reversion rule: %#v", strategy.Entries.Long)
	}
	if strategy.Entries.Short.When.All[0].Price.Source != PriceHigh || strategy.Entries.Short.When.All[1].Price.Relation != PriceCrossesBelow {
		t.Fatalf("unexpected short reversion rule: %#v", strategy.Entries.Short)
	}
}

func TestNewSessionBreakout(t *testing.T) {
	strategy := NewSessionBreakout()
	if errs := strategy.Validate(primitive.DefaultRegistry); len(errs) != 0 {
		t.Fatalf("template is invalid: %#v", errs)
	}
	if strategy.Template.ID != SessionBreakoutTemplateID || strategy.Template.Version != SessionBreakoutTemplateVersion {
		t.Fatalf("template identity = %#v", strategy.Template)
	}
	if strategy.Timeframes["entry"] != core.TFM15 || strategy.Entries.Long.When.Price.Relation != PriceCrossesAbove || strategy.Entries.Short.When.Price.Relation != PriceCrossesBelow {
		t.Fatalf("unexpected session breakout rules: %#v", strategy)
	}
}

func TestNewLiquiditySweepStructure(t *testing.T) {
	strategy := NewLiquiditySweepStructure()
	if errs := strategy.Validate(primitive.DefaultRegistry); len(errs) != 0 {
		t.Fatalf("template is invalid: %#v", errs)
	}
	if strategy.Template.ID != LiquiditySweepStructureTemplateID || strategy.Template.Version != LiquiditySweepStructureTemplateVersion {
		t.Fatalf("template identity = %#v", strategy.Template)
	}
	if strategy.Timeframes["entry"] != core.TFH1 || strategy.Entries.Long.When.After == nil || strategy.Entries.Short.When.After == nil {
		t.Fatalf("unexpected confirmation rules: %#v", strategy)
	}
	if strategy.Entries.Long.When.After.WithinBars != 5 || strategy.Nodes[0].Type != "liquidity_sweep_bullish" || strategy.Nodes[3].Type != "market_structure_bearish" {
		t.Fatalf("unexpected directional nodes: %#v", strategy)
	}
}

// These hashes are golden fixtures for the six supported starting strategies.
// They make an unintended rule, parameter, timing, or direction change visible
// in review, while Validate above ensures the fixture still compiles.
func TestPopularTemplatesGoldenStrategies(t *testing.T) {
	tests := []struct {
		name string
		new  func() *Strategy
		hash string
	}{
		{"moving-average crossover", NewMovingAverageCrossover, "a86b1f91ae8cfb97d1470925ab294c54c9ec3eb2a3b556011f81f5fb0462be46"},
		{"RSI pullback", NewRSIPullback, "72336e8a796dec2e6a0a49748f88ae327c3fb23b744473a00ef92c6bf109877f"},
		{"Donchian breakout", NewDonchianBreakout, "e9473c54432d6766db3bd4eeb34103100017e003c3685d66db8efdce08fcfc86"},
		{"Bollinger reversion", NewBollingerReversion, "709cbc3bebe8fa5ffc2c0c566cbc042c1023e41b76eff5444cf456c9d840fb1d"},
		{"session breakout", NewSessionBreakout, "8a891a9e8c8aee3903caf097586b644cd956838fad2f4afccfc8d4cc0673f58c"},
		{"liquidity sweep structure", NewLiquiditySweepStructure, "67fa1871d595cccfe06fc2b09d9181de0887678b0adaca41c2c62f89c177fdc2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy := tt.new()
			if errs := strategy.Validate(primitive.DefaultRegistry); len(errs) != 0 {
				t.Fatalf("template is invalid: %#v", errs)
			}
			if got := strategy.Hash(); got != tt.hash {
				t.Fatalf("strategy hash = %s, want %s", got, tt.hash)
			}
		})
	}
}

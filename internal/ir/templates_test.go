package ir

import (
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	_ "github.com/Bcypher01/Assay/internal/packs/indicator"
	_ "github.com/Bcypher01/Assay/internal/packs/smc"
	_ "github.com/Bcypher01/Assay/internal/packs/timeofday"
	"github.com/Bcypher01/Assay/internal/primitive"
)

func TestNewMovingAverageCrossover(t *testing.T) {
	strategy := NewMovingAverageCrossover()
	if errs := strategy.Validate(primitive.DefaultRegistry); len(errs) != 0 {
		t.Fatalf("template is invalid: %#v", errs)
	}
	if strategy.Schema != SchemaV2 || strategy.Template.ID != MovingAverageCrossoverTemplateID || strategy.Template.Version != MovingAverageCrossoverTemplateVersion {
		t.Fatalf("template identity = %#v", strategy)
	}
	if strategy.Universe.Symbols[0] != "EUR/USD" || strategy.Timeframes["entry"] != core.TFD1 {
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
		{"moving-average crossover", NewMovingAverageCrossover, "c9294078d71893c2cdd2e633323b46e9984c914446c2163dafd68355a5a2523d"},
		{"RSI pullback", NewRSIPullback, "c2f485751b1d1691a7b7affd69300e2a7dd49de1019bd1c7ee57c311ededd841"},
		{"Donchian breakout", NewDonchianBreakout, "510e1a120ef7b10cf231fb027873bf59bde0865d554060545821ccce7106b7ff"},
		{"Bollinger reversion", NewBollingerReversion, "41805a597c5bab4e7ac5ece1a6ac78db75bafe1c8723bac853d9216c945b5bfd"},
		{"session breakout", NewSessionBreakout, "861be070f4718c1db2666faeb90f65dc36eb571bb01005f1f8c847746544b386"},
		{"liquidity sweep structure", NewLiquiditySweepStructure, "06dd62e97d33713b870b613fee64fe0c8fce23781f172e9249ac579917b9c5c8"},
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

package ir

import (
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	_ "github.com/Bcypher01/Assay/internal/packs/indicator"
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

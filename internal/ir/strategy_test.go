package ir

import (
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	_ "github.com/Bcypher01/Assay/internal/packs/indicator"
	"github.com/Bcypher01/Assay/internal/primitive"
)

func validStrategy() *Strategy {
	return &Strategy{Schema: SchemaV1, Name: "test", Universe: Universe{Symbols: []string{"X"}}, Direction: DirectionLong, Timeframes: map[string]core.TF{"bias": core.TFM1, "poi": core.TFM1, "entry": core.TFM1}, Risk: Risk{Stop: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(1)}, Target: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(2)}}, Nodes: []Node{{ID: "a", Pack: "indicator", Type: "ma_touch"}, {ID: "stop", Pack: "indicator", Type: "sma"}, {ID: "target", Pack: "indicator", Type: "ema"}}, Slots: Slots{Bias: SlotSpec{All: []string{"a"}}, POI: SlotSpec{}, Trigger: SlotSpec{}, Invalidation: "stop", Target: "target"}}
}
func TestValidateCollectsFieldErrors(t *testing.T) {
	s := validStrategy()
	s.Nodes[0].Type = "rsi"
	s.Slots.Trigger.All = []string{"missing", "a", "a", "a"}
	s.Timeframes["entry"] = core.TFH1
	errs := s.Validate(primitive.DefaultRegistry)
	if len(errs) < 3 {
		t.Fatalf("got %#v", errs)
	}
}
func TestHashCanonicalizesMapOrder(t *testing.T) {
	a, b := validStrategy(), validStrategy()
	a.Timeframes = map[string]core.TF{"entry": core.TFM1, "bias": core.TFM1, "poi": core.TFM1}
	if a.Hash() != b.Hash() {
		t.Fatalf("hash differs: %s %s", a.Hash(), b.Hash())
	}
	b.Name = "other"
	if a.Hash() == b.Hash() {
		t.Fatal("changed strategy has same hash")
	}
}

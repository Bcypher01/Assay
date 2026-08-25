package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
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

func TestValidateExplainsMissingLevelSlots(t *testing.T) {
	s := validStrategy()
	s.Slots.Invalidation = ""
	s.Slots.Target = ""
	errs := s.Validate(primitive.DefaultRegistry)
	if len(errs) != 0 {
		t.Fatalf("distance-based exits should not need level slots: %#v", errs)
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

func TestV1HashRemainsCompatibleWithPreV2CanonicalShape(t *testing.T) {
	s := validStrategy()
	legacy := struct {
		Schema     string             `json:"schema"`
		Name       string             `json:"name"`
		Universe   Universe           `json:"universe"`
		Timeframes map[string]core.TF `json:"timeframes"`
		Risk       Risk               `json:"risk"`
		Direction  Direction          `json:"direction"`
		Nodes      []Node             `json:"nodes"`
		Slots      Slots              `json:"slots"`
		Edges      []Edge             `json:"edges,omitempty"`
	}{s.Schema, s.Name, s.Universe, s.Timeframes, s.Risk, s.Direction, s.Nodes, s.Slots, s.Edges}
	b, err := canonicalJSON(legacy)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if got, want := s.Hash(), hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("v1 hash changed: got %s, want %s", got, want)
	}
}

func TestValidateV2Entries(t *testing.T) {
	s := validStrategy()
	s.Schema = SchemaV2
	s.Direction = ""
	s.Slots = Slots{}
	s.Template = Template{ID: "ma-crossover", Version: 1}
	s.Entries = Entries{
		Timing: EntryTimingNextOpen,
		Long:   EntryBranch{Enabled: true, When: Expression{Node: "a"}},
	}
	if errs := s.Validate(primitive.DefaultRegistry); len(errs) != 0 {
		t.Fatalf("valid v2 entries rejected: %#v", errs)
	}
}

func TestValidateV2ReportsExactExpressionErrors(t *testing.T) {
	s := validStrategy()
	s.Schema = SchemaV2
	s.Direction = DirectionLong
	s.Slots = Slots{}
	s.Template = Template{}
	s.Entries = Entries{
		Timing: "at_close",
		Long: EntryBranch{Enabled: true, When: Expression{
			Node: "stop",
			Any:  []Expression{{Node: "missing"}},
		}},
	}
	errs := s.Validate(primitive.DefaultRegistry)
	paths := make([]string, 0, len(errs))
	for _, err := range errs {
		paths = append(paths, err.Path)
	}
	want := []string{"direction", "entries.timing", "entries.long.when", "template.id", "template.version"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestMigrateV1ToV2PreservesBranchesAndHash(t *testing.T) {
	s := validStrategy()
	s.Direction = DirectionBoth
	migrated, err := s.MigrateV1ToV2()
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Schema != SchemaV2 || migrated.Direction != "" || !migrated.Entries.Long.Enabled || !migrated.Entries.Short.Enabled {
		t.Fatalf("bad migration: %#v", migrated)
	}
	if errs := migrated.Validate(primitive.DefaultRegistry); len(errs) != 0 {
		t.Fatalf("migrated document invalid: %#v", errs)
	}
	again, err := s.MigrateV1ToV2()
	if err != nil || migrated.Hash() != again.Hash() {
		t.Fatalf("migration is not deterministic: %q, %v", again.Hash(), err)
	}
}

func TestMigrateV1ToV2RejectsTemporalTrigger(t *testing.T) {
	s := validStrategy()
	s.Slots.Trigger.WithinBars = 1
	if _, err := s.MigrateV1ToV2(); err == nil {
		t.Fatal("withinBars migration unexpectedly succeeded")
	}
}

package ir

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
)

func v2Strategy() *Strategy {
	s := validStrategy()
	s.Schema = SchemaV2
	s.Direction = ""
	s.Slots = Slots{}
	s.Template = Template{ID: "ma-crossover", Version: 1}
	s.Entries = Entries{
		Timing: EntryTimingNextOpen,
		Long:   EntryBranch{Enabled: true, Name: "Long crossover", When: Expression{Node: "a"}},
	}
	return s
}

// A document with no order must serialise, and so hash, exactly as it did
// before order types existed.
func TestAbsentOrderKeepsTheDocumentByteIdentical(t *testing.T) {
	s := v2Strategy()
	before := s.Hash()
	encoded, err := json.Marshal(s.Entries)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "order") {
		t.Fatalf("absent order leaked into the document: %s", encoded)
	}
	s.Entries.Order = &Order{Type: OrderLimit, Node: "stop"}
	if s.Hash() == before {
		t.Fatal("an order did not change the document hash")
	}
}

func TestValidateOrder(t *testing.T) {
	ticks := func(n int64) *core.Distance {
		return &core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(n)}
	}
	cases := []struct {
		name     string
		order    *Order
		wantPath string
	}{
		{name: "no order at all"},
		{name: "explicit market", order: &Order{Type: OrderMarket}},
		// "stop" and "target" are level primitives in the shared fixture.
		{name: "limit at a level node", order: &Order{Type: OrderLimit, Node: "stop"}},
		{name: "stop at an offset", order: &Order{Type: OrderStop, Offset: ticks(5)}},
		{
			name:     "market carrying a level",
			order:    &Order{Type: OrderMarket, Node: "stop"},
			wantPath: "entries.order.type",
		},
		{name: "unknown type", order: &Order{Type: "iceberg"}, wantPath: "entries.order.type"},
		{name: "limit with no level", order: &Order{Type: OrderLimit}, wantPath: "entries.order"},
		{
			name:     "both a level and an offset",
			order:    &Order{Type: OrderLimit, Node: "stop", Offset: ticks(5)},
			wantPath: "entries.order",
		},
		{
			name:     "negative offset",
			order:    &Order{Type: OrderLimit, Offset: ticks(-5)},
			wantPath: "entries.order.offset",
		},
		{
			name:     "unknown level node",
			order:    &Order{Type: OrderLimit, Node: "missing"},
			wantPath: "entries.order.node",
		},
		{
			name:     "a boolean node cannot be a price",
			order:    &Order{Type: OrderLimit, Node: "a"},
			wantPath: "entries.order.node",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := v2Strategy()
			s.Entries.Order = c.order
			var paths []string
			for _, e := range s.Validate(primitive.DefaultRegistry) {
				paths = append(paths, e.Path)
			}
			if c.wantPath == "" {
				if len(paths) != 0 {
					t.Fatalf("valid order rejected: %v", paths)
				}
				return
			}
			for _, p := range paths {
				if p == c.wantPath {
					return
				}
			}
			t.Fatalf("want an error at %s, got %v", c.wantPath, paths)
		})
	}
}

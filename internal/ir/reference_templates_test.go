package ir

import (
	"testing"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
)

// Every catalogue entry must be executable input, not a card that fails the
// moment it is opened. This covers additions the per-template tests do not.
func TestPopularTemplatesAreAllValidAndDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range PopularTemplates() {
		t.Run(def.ID, func(t *testing.T) {
			if seen[def.ID] {
				t.Fatalf("duplicate template id %q", def.ID)
			}
			seen[def.ID] = true
			if def.Name == "" || def.Family == "" || def.Description == "" || def.ExpectedFrequency == "" {
				t.Fatalf("catalogue metadata is incomplete: %#v", def)
			}
			strategy := def.New()
			if errs := strategy.Validate(primitive.DefaultRegistry); len(errs) != 0 {
				t.Fatalf("template is invalid: %#v", errs)
			}
			if strategy.Template.ID != def.ID || strategy.Template.Version != def.Version {
				t.Fatalf("catalogue identity does not match the document: %#v", strategy.Template)
			}
			// A fresh document every call: a caller that edits one must not
			// change what the next request receives.
			if def.New() == strategy {
				t.Fatal("template returned a shared document")
			}
		})
	}
}

func TestNewPreviousDayHighBreak(t *testing.T) {
	s := NewPreviousDayHighBreak()
	if errs := s.Validate(primitive.DefaultRegistry); len(errs) != 0 {
		t.Fatalf("template is invalid: %#v", errs)
	}
	// The whole point: a daily level under a fifteen-minute entry.
	if s.Timeframes["entry"] != core.TFM15 || s.Nodes[0].Timeframe != core.TFD1 {
		t.Fatalf("the level must be coarser than the entry chart: %#v", s.Timeframes)
	}
	if s.Nodes[0].Type != "period_high" {
		t.Fatalf("unexpected level node: %#v", s.Nodes)
	}
	price := s.Entries.Long.When.Price
	if price == nil || price.Node != "previous-day-high" || price.Source != PriceClose || price.Relation != PriceCrossesAbove {
		t.Fatalf("unexpected entry rule: %#v", s.Entries.Long.When)
	}
	if s.Entries.Short.Enabled {
		t.Fatal("a break of the previous high is a long-only rule")
	}
}

func TestNewOpeningRangeBreak(t *testing.T) {
	s := NewOpeningRangeBreak()
	if errs := s.Validate(primitive.DefaultRegistry); len(errs) != 0 {
		t.Fatalf("template is invalid: %#v", errs)
	}
	if len(s.Nodes) != 2 || s.Nodes[0].Type != "opening_range_high" || s.Nodes[1].Type != "opening_range_low" {
		t.Fatalf("unexpected level nodes: %#v", s.Nodes)
	}
	if !s.Entries.Long.Enabled || !s.Entries.Short.Enabled {
		t.Fatal("an opening range breaks in both directions")
	}
	if s.Entries.Long.When.Price.Relation != PriceCrossesAbove || s.Entries.Short.When.Price.Relation != PriceCrossesBelow {
		t.Fatalf("unexpected relations: %#v", s.Entries)
	}
}

// The reference templates are pinned like the rest of the catalogue: a change
// to one is a change to the study identity of everyone who started from it.
func TestReferenceTemplateHashesAreStable(t *testing.T) {
	for _, tt := range []struct {
		name string
		new  func() *Strategy
		hash string
	}{
		{"previous day high", NewPreviousDayHighBreak, "952ec22c3b6bfc99a237b5aab1e31fd8dcf735acfd34207471251a58037f3821"},
		{"opening range", NewOpeningRangeBreak, "6561a531cad3e8773e5df003184b371676ec834731cf369fe7a13110c745a635"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.new().Hash(); got != tt.hash {
				t.Fatalf("strategy hash = %s, want %s", got, tt.hash)
			}
		})
	}
}

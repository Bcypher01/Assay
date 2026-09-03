package ir

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
)

func half() core.Ratio { return core.One / 2 }

// A document without management must serialise, and therefore hash, exactly as
// it did before management existed. Run identity cannot move because the
// application was upgraded.
func TestAbsentManagementKeepsTheDocumentByteIdentical(t *testing.T) {
	s := validStrategy()
	before := s.Hash()
	encoded, err := json.Marshal(s.Risk)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "management") {
		t.Fatalf("absent management leaked into the document: %s", encoded)
	}
	if s.Hash() != before {
		t.Fatal("hashing is not stable")
	}

	managed := validStrategy()
	managed.Risk.Management = &Management{Close: half()}
	if managed.Hash() == before {
		t.Fatal("management did not change the document hash")
	}
}

func TestManagementRoundTripsThroughTheAuthoringForm(t *testing.T) {
	want := Management{
		Close:     half(),
		BreakEven: true,
		Trail:     core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(15)},
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	// The close fraction is a decimal at the boundary, not a scaled integer.
	if !strings.Contains(string(encoded), `"close":0.5`) {
		t.Fatalf("close is not readable: %s", encoded)
	}
	var got Management
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip lost information: %#v vs %#v", got, want)
	}
}

func TestManagementOmitsWhatIsNotSet(t *testing.T) {
	encoded, err := json.Marshal(Management{Close: half()})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(encoded); got != `{"close":0.5}` {
		t.Fatalf("want a minimal document, got %s", got)
	}
}

func TestValidateManagement(t *testing.T) {
	pips := func(n int64) core.Distance {
		return core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(n)}
	}
	cases := []struct {
		name       string
		management Management
		wantPath   string
	}{
		{name: "partial close and trail", management: Management{Close: half(), Trail: pips(10)}},
		{name: "full close", management: Management{Close: core.One}},
		{name: "trail only", management: Management{Trail: pips(10)}},
		{name: "partial with break-even", management: Management{Close: half(), BreakEven: true}},
		{name: "close above one", management: Management{Close: core.One * 2}, wantPath: "risk.management.close"},
		{name: "negative close", management: Management{Close: -half()}, wantPath: "risk.management.close"},
		{
			name:       "break-even without a partial close",
			management: Management{Close: core.One, BreakEven: true},
			wantPath:   "risk.management.breakEven",
		},
		{
			name:       "atr trail",
			management: Management{Trail: core.Distance{Unit: core.UnitATR, Value: core.RatioFromInt(2)}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := validStrategy()
			s.Risk.Management = &c.management
			var paths []string
			for _, e := range s.Validate(primitive.DefaultRegistry) {
				paths = append(paths, e.Path)
			}
			if c.wantPath == "" {
				if len(paths) != 0 {
					t.Fatalf("valid management was rejected: %v", paths)
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

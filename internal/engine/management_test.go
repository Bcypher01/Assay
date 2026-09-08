package engine

import (
	"testing"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/ir"
	"github.com/bcypher01/assay/internal/sim"
)

func newSignals(n int) *sim.Signals {
	return &sim.Signals{Enter: core.NewBitset(n)}
}

// The engine's job here is wiring only: whatever the document says about trade
// management must arrive on the entry the simulator executes, on both the
// level-based and the distance-based exit paths.
func TestManagementReachesTheSimulatedEntry(t *testing.T) {
	trail := core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(15)}
	m := &ir.Management{Close: core.One / 2, BreakEven: true, Trail: trail}

	levels := newSignals(4)
	addSide(levels, core.SideLong, 1, 90, 110, entryPlan{management: m})
	distances := newSignals(4)
	addRelativeSide(distances, core.SideLong, 1, trail, trail, entryPlan{management: m})

	for _, c := range []struct {
		name string
		sig  *sim.Signals
	}{{"levels", levels}, {"distances", distances}} {
		plan := c.sig.TargetPlan[0]
		if plan.Close != core.One/2 || !plan.MoveStopToBreakEven || plan.TrailSpec != trail {
			t.Fatalf("%s path dropped management: %#v", c.name, plan)
		}
	}
	if levels.TargetPlan[0].Level != 110 {
		t.Fatalf("the level path lost its target: %#v", levels.TargetPlan[0])
	}
}

// A document with no management must produce exactly the plan the engine
// produced before management existed.
func TestAbsentManagementLeavesThePlanBare(t *testing.T) {
	sig := newSignals(4)
	addSide(sig, core.SideLong, 1, 90, 110, entryPlan{})
	if got := sig.TargetPlan[0]; got != (sim.TargetPlan{Level: 110}) {
		t.Fatalf("want a bare plan, got %#v", got)
	}
}

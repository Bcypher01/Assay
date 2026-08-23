package core

import "testing"

func TestDistanceTicks(t *testing.T) {
	const pipTicks = 10
	ref := Price(108_532) // EURUSD at 1.08532, tick 0.00001
	atr := Price(120)     // 12 pips

	cases := []struct {
		name string
		d    Distance
		want Price
	}{
		{"ticks", Distance{UnitTicks, RatioFromInt(25)}, 25},
		{"pips", Distance{UnitPips, RatioFromFloat(1.5)}, 15},
		{"atr multiple", Distance{UnitATR, RatioFromFloat(0.25)}, 30},
		{"percent", Distance{UnitPercent, RatioFromFloat(0.5)}, 543},
		{"zero", Distance{UnitATR, 0}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.d.Ticks(pipTicks, ref, atr); got != c.want {
				t.Fatalf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestDistanceRequirements(t *testing.T) {
	if !(Distance{Unit: UnitPercent}).NeedsRef() {
		t.Fatal("percent needs a reference price")
	}
	if !(Distance{Unit: UnitATR}).NeedsATR() {
		t.Fatal("atr needs an atr value")
	}
	if (Distance{Unit: UnitTicks}).NeedsRef() || (Distance{Unit: UnitTicks}).NeedsATR() {
		t.Fatal("ticks needs neither")
	}
}

func TestUnitParseRoundTrip(t *testing.T) {
	for u := UnitTicks; u < unitCount; u++ {
		got, err := ParseUnit(u.String())
		if err != nil || got != u {
			t.Fatalf("round trip of %v gave (%v, %v)", u, got, err)
		}
	}
	if _, err := ParseUnit("furlongs"); err == nil {
		t.Fatal("expected an error")
	}
}

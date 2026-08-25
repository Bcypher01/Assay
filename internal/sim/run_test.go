package sim

import (
	"context"
	"reflect"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/instrument"
)

func fixtureSeries(tf core.TF, bars [][4]core.Price) *core.Series {
	s := core.NewSeries("X", tf, len(bars))
	for i, b := range bars {
		s.Append(core.Time(int64(i)*tf.Millis()), b[0], b[1], b[2], b[3], 0)
	}
	return s
}

func TestRunDeterministic(t *testing.T) {
	s := fixtureSeries(core.TFM1, [][4]core.Price{{100, 101, 99, 100}, {100, 110, 90, 100}})
	b := core.NewBitset(2)
	b.Set(0)
	sig := Signals{Enter: b, Side: []core.Side{core.SideLong}, StopLevel: []core.Price{95}, TargetPlan: []TargetPlan{{Level: 105}}}
	var a, c RunResult
	if err := Run(context.Background(), s, nil, sig, fixtureConfig(), &a); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), s, nil, sig, fixtureConfig(), &c); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, c) {
		t.Fatalf("same inputs produced different results:\n%#v\n%#v", a, c)
	}
}

func FuzzRunInvariants(f *testing.F) {
	f.Add(uint8(100), uint8(110), uint8(90), uint8(100))
	f.Fuzz(func(t *testing.T, open, high, low, close uint8) {
		hi, lo := core.Price(high), core.Price(low)
		if hi < lo {
			hi, lo = lo, hi
		}
		o := core.Price(open)
		if o < lo {
			o = lo
		}
		if o > hi {
			o = hi
		}
		c := core.Price(close)
		if c < lo {
			c = lo
		}
		if c > hi {
			c = hi
		}
		s := fixtureSeries(core.TFM1, [][4]core.Price{{100, 100, 100, 100}, {o, hi, lo, c}})
		b := core.NewBitset(2)
		b.Set(0)
		sig := Signals{Enter: b, Side: []core.Side{core.SideLong}, StopLevel: []core.Price{95}, TargetPlan: []TargetPlan{{Level: 105}}}
		var out RunResult
		if err := Run(context.Background(), s, nil, sig, fixtureConfig(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Equity) != s.Len() || len(out.EquityOpt) != s.Len() {
			t.Fatal("equity is not continuous")
		}
		for _, tr := range out.Trades {
			if tr.Pessimistic.ExitBar < tr.EntryBar || tr.Optimistic.ExitBar < tr.EntryBar {
				t.Fatal("trade closed before opening")
			}
			if tr.Pessimistic.Net > tr.Optimistic.Net {
				t.Fatal("pessimistic outcome exceeds optimistic outcome")
			}
		}
	})
}

func fixtureConfig() Config {
	return Config{Spec: instrument.Spec{Symbol: "X", TickSize: "1", Gap: instrument.GapFillAtOpen}, Risk: RiskModel{InitialEquity: 1_000, FixedLots: core.One}}
}

func TestRunAmbiguousBarCarriesBounds(t *testing.T) {
	s := fixtureSeries(core.TFM1, [][4]core.Price{{100, 101, 99, 100}, {100, 110, 90, 100}})
	b := core.NewBitset(2)
	b.Set(0)
	sig := Signals{Enter: b, Side: []core.Side{core.SideLong}, StopLevel: []core.Price{95}, TargetPlan: []TargetPlan{{Level: 105}}}
	var got RunResult
	if err := Run(context.Background(), s, nil, sig, fixtureConfig(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Unresolved != 1 || len(got.Trades) != 1 || !got.Trades[0].Ambiguous {
		t.Fatalf("ambiguity was not recorded: %#v", got)
	}
	if got.Trades[0].Pessimistic.Net >= got.Trades[0].Optimistic.Net {
		t.Fatalf("bounds did not widen: %#v", got.Trades[0])
	}
}

func TestRunResolvesPipOffsetsFromActualEntryFill(t *testing.T) {
	s := fixtureSeries(core.TFM1, [][4]core.Price{{100, 100, 100, 100}, {103, 110, 90, 103}})
	b := core.NewBitset(2)
	b.Set(0)
	sig := Signals{
		Enter: b, Side: []core.Side{core.SideLong}, StopLevel: []core.Price{0}, TargetPlan: []TargetPlan{{}},
		StopDistance: []core.Price{5}, TargetDistance: []core.Price{10},
	}
	var out RunResult
	if err := Run(context.Background(), s, nil, sig, fixtureConfig(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Trades) != 1 || out.Trades[0].EntryPrice != 103 || out.Trades[0].Stop != 98 || out.Trades[0].Target != 113 {
		t.Fatalf("relative exit levels = %#v", out.Trades)
	}
}

func TestRunResolvesPercentOffsetsFromActualEntryFill(t *testing.T) {
	s := fixtureSeries(core.TFM1, [][4]core.Price{{100, 100, 100, 100}, {200, 206, 199, 204}})
	b := core.NewBitset(2)
	b.Set(0)
	sig := Signals{
		Enter: b, Side: []core.Side{core.SideLong}, StopLevel: []core.Price{0}, TargetPlan: []TargetPlan{{}},
		StopSpec:   []core.Distance{{Unit: core.UnitPercent, Value: core.RatioFromInt(1)}},
		TargetSpec: []core.Distance{{Unit: core.UnitPercent, Value: core.RatioFromInt(3)}},
	}
	var out RunResult
	if err := Run(context.Background(), s, nil, sig, fixtureConfig(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Trades) != 1 || out.Trades[0].EntryPrice != 200 || out.Trades[0].Stop != 198 || out.Trades[0].Target != 206 {
		t.Fatalf("percentage exit levels = %#v", out.Trades)
	}
	if out.Trades[0].Pessimistic.Net <= 0 {
		t.Fatalf("target exit should be profitable: %#v", out.Trades[0])
	}
}

func TestRunLongTargetOnlyBarIsAWin(t *testing.T) {
	s := fixtureSeries(core.TFM1, [][4]core.Price{{100, 100, 100, 100}, {100, 106, 99, 104}})
	b := core.NewBitset(2)
	b.Set(0)
	sig := Signals{Enter: b, Side: []core.Side{core.SideLong}, StopLevel: []core.Price{95}, TargetPlan: []TargetPlan{{Level: 105}}}
	var out RunResult
	if err := Run(context.Background(), s, nil, sig, fixtureConfig(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Trades) != 1 || out.Trades[0].Ambiguous || out.Trades[0].Pessimistic.Net <= 0 {
		t.Fatalf("long target-only bar should close as a resolved win: %#v", out.Trades)
	}
}

func TestRunShortTargetOnlyBarIsAWin(t *testing.T) {
	s := fixtureSeries(core.TFM1, [][4]core.Price{{100, 100, 100, 100}, {100, 101, 94, 96}})
	b := core.NewBitset(2)
	b.Set(0)
	sig := Signals{Enter: b, Side: []core.Side{core.SideShort}, StopLevel: []core.Price{105}, TargetPlan: []TargetPlan{{Level: 95}}}
	var out RunResult
	if err := Run(context.Background(), s, nil, sig, fixtureConfig(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Trades) != 1 || out.Trades[0].Ambiguous || out.Trades[0].Pessimistic.Net <= 0 {
		t.Fatalf("short target-only bar should close as a resolved win: %#v", out.Trades)
	}
}

func TestRunFineSeriesNarrowsAmbiguity(t *testing.T) {
	s := fixtureSeries(core.TFM1, [][4]core.Price{{100, 101, 99, 100}, {100, 110, 90, 100}})
	f := fixtureSeries(core.TFS1, [][4]core.Price{{100, 101, 99, 100}, {100, 106, 100, 105}, {105, 105, 90, 95}})
	// Place fine bars inside the second minute, after a harmless first bar.
	f.Time[0], f.Time[1], f.Time[2] = 60_000, 61_000, 62_000
	b := core.NewBitset(2)
	b.Set(0)
	sig := Signals{Enter: b, Side: []core.Side{core.SideLong}, StopLevel: []core.Price{95}, TargetPlan: []TargetPlan{{Level: 105}}}
	var got RunResult
	if err := Run(context.Background(), s, f, sig, fixtureConfig(), &got); err != nil {
		t.Fatal(err)
	}
	tr := got.Trades[0]
	if got.Unresolved != 0 || tr.Pessimistic.Net != tr.Optimistic.Net || tr.Pessimistic.ExitPrice != 105 {
		t.Fatalf("fine replay did not resolve target-first ordering: %#v", got)
	}
}

func TestEntryOrdersRespectGapPolicy(t *testing.T) {
	s := fixtureSeries(core.TFM1, [][4]core.Price{{100, 100, 100, 100}, {90, 92, 88, 91}, {91, 91, 91, 91}})
	b := core.NewBitset(3)
	b.Set(0)
	sig := Signals{Enter: b, Side: []core.Side{core.SideLong}, StopLevel: []core.Price{80}, TargetPlan: []TargetPlan{{}}, Orders: []Order{{Type: OrderLimit, Level: 95}}}
	cfg := fixtureConfig()
	cfg.Spec.Gap = instrument.GapFillAtOpen
	var open RunResult
	if err := Run(context.Background(), s, nil, sig, cfg, &open); err != nil {
		t.Fatal(err)
	}
	if open.Trades[0].EntryPrice != 90 {
		t.Fatalf("open fill = %d", open.Trades[0].EntryPrice)
	}
	cfg.Spec.Gap = instrument.GapFillAtLevel
	var level RunResult
	if err := Run(context.Background(), s, nil, sig, cfg, &level); err != nil {
		t.Fatal(err)
	}
	if level.Trades[0].EntryPrice != 95 {
		t.Fatalf("level fill = %d", level.Trades[0].EntryPrice)
	}
}

func TestSizingRoundsDownAndSkipsZeroLot(t *testing.T) {
	s := fixtureSeries(core.TFM1, [][4]core.Price{{100, 100, 100, 100}, {100, 100, 100, 100}})
	b := core.NewBitset(2)
	b.Set(0)
	sig := Signals{Enter: b, Side: []core.Side{core.SideLong}, StopLevel: []core.Price{90}, TargetPlan: []TargetPlan{{}}}
	cfg := fixtureConfig()
	cfg.Spec.LotStep = "0.1"
	cfg.Spec.MinLot = "0.1"
	cfg.Risk = RiskModel{InitialEquity: 100, RiskPerTrade: core.RatioFromFloat(.015)}
	var out RunResult
	if err := Run(context.Background(), s, nil, sig, cfg, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Trades) != 1 || out.Trades[0].Lots != core.RatioFromFloat(.2) {
		t.Fatalf("unexpected rounded size: %#v", out.Trades)
	}
	cfg.Risk.RiskPerTrade = core.RatioFromFloat(.001)
	if err := Run(context.Background(), s, nil, sig, cfg, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Trades) != 0 {
		t.Fatalf("sub-minimum size opened: %#v", out.Trades)
	}
}

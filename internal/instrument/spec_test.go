package instrument

import (
	"errors"
	"strings"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
)

func TestParseDecimal(t *testing.T) {
	cases := []struct {
		in       string
		wantMant int64
		wantFrac int
		wantErr  bool
	}{
		{"1.08532", 108532, 5, false},
		{"0.00001", 1, 5, false},
		{"67000.5", 670005, 1, false},
		{"-12.25", -1225, 2, false},
		{"+3", 3, 0, false},
		{"100", 100, 0, false},
		{".5", 5, 1, false},
		{"", 0, 0, true},
		{"1.2.3", 0, 0, true},
		{"1e5", 0, 0, true},
		{"abc", 0, 0, true},
		{"-", 0, 0, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			m, f, err := parseDecimal(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", c.in)
				}
				if !errors.Is(err, ErrBadDecimal) {
					t.Fatalf("error should wrap ErrBadDecimal, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m != c.wantMant || f != c.wantFrac {
				t.Fatalf("got (%d, %d), want (%d, %d)", m, f, c.wantMant, c.wantFrac)
			}
		})
	}
}

// TestPriceRoundTripIsExact is the batch's headline exit criterion: every
// sanctioned tick size, including ones that are not powers of ten and ones a
// binary float cannot represent, must survive decimal -> Price -> decimal
// unchanged.
func TestPriceRoundTripIsExact(t *testing.T) {
	r := MustDefault()
	samples := map[string][]string{
		"DUKAS:EURUSD":    {"1.08532", "0.00001", "1.00000", "1.23456", "0.99999"},
		"DUKAS:USDJPY":    {"147.250", "0.001", "100.000"},
		"DUKAS:XAUUSD":    {"2431.750", "0.001"},
		"BINANCE:BTCUSDT": {"67000.50", "0.01", "104233.99"},
		"BINANCE:ETHUSDT": {"3211.07", "0.01"},
	}
	for sym, vals := range samples {
		spec, err := r.Get(sym)
		if err != nil {
			t.Fatalf("%s: %v", sym, err)
		}
		for _, v := range vals {
			p, err := spec.PriceFromDecimal(v)
			if err != nil {
				t.Fatalf("%s %s: %v", sym, v, err)
			}
			if got := spec.PriceToDecimal(p); got != v {
				t.Fatalf("%s: %s round-tripped to %s (ticks=%d)", sym, v, got, p)
			}
		}
	}
}

func TestPriceRejectsNonMultipleOfTick(t *testing.T) {
	spec, err := MustDefault().Get("BINANCE:BTCUSDT") // tick 0.01
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spec.PriceFromDecimal("67000.005"); err == nil {
		t.Fatal("a price finer than the tick size must be rejected, not rounded")
	}
	if _, err := spec.PriceFromDecimal("67000.05"); err != nil {
		t.Fatalf("a valid multiple was rejected: %v", err)
	}
}

func TestPriceIsExactWhereFloatIsNot(t *testing.T) {
	// 0.00001 has no exact binary representation. Routing through float64
	// would accumulate error across a series; ticks do not.
	spec, _ := MustDefault().Get("DUKAS:EURUSD")
	var acc core.Price
	one, err := spec.PriceFromDecimal("0.00001")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100000; i++ {
		acc += one
	}
	if got := spec.PriceToDecimal(acc); got != "1.00000" {
		t.Fatalf("100000 ticks summed to %s, want 1.00000", got)
	}
}

func TestSpecValidation(t *testing.T) {
	bad := []struct {
		name string
		spec Spec
		want string
	}{
		{"no symbol", Spec{TickSize: "0.01"}, "symbol"},
		{"no tick size", Spec{Symbol: "X"}, "tickSize"},
		{"zero tick size", Spec{Symbol: "X", TickSize: "0"}, "tickSize"},
		{"negative tick size", Spec{Symbol: "X", TickSize: "-0.01"}, "tickSize"},
		{"bad lot step", Spec{Symbol: "X", TickSize: "0.01", LotStep: "many"}, "lotStep"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			err := c.spec.Validate()
			if err == nil {
				t.Fatal("expected an error")
			}
			var fe *FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("error should name a field, got %v", err)
			}
			if !strings.Contains(fe.Field, c.want) {
				t.Fatalf("field = %q, want it to mention %q", fe.Field, c.want)
			}
		})
	}
}

func TestSpecDefaults(t *testing.T) {
	s := Spec{Symbol: "X", TickSize: "0.01"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.PipTicks != 1 || s.Gap != GapFillAtOpen || s.Quality != QualityM1 {
		t.Fatal("defaults were not applied")
	}
	if s.Calendar == nil || !s.Calendar.IsOpen(core.MustParseTime("2026-08-22T03:00:00Z")) {
		t.Fatal("a spec with no calendar should default to continuous")
	}
}

func TestQualityTierSupports(t *testing.T) {
	if !QualityM1.Supports(core.TFM15) {
		t.Fatal("m1 data supports m15 strategies")
	}
	if QualityM1.Supports(core.TFS1) {
		t.Fatal("m1 data cannot honestly serve a 1-second strategy")
	}
	if !QualityTick.Supports(core.TFTick) {
		t.Fatal("tick data supports tick strategies")
	}
	if QualityDaily.Supports(core.TFH1) {
		t.Fatal("daily data cannot serve an hourly strategy")
	}
}

func TestRegistryLoad(t *testing.T) {
	r := MustDefault()
	if r.Len() < 6 {
		t.Fatalf("expected the seed set, got %d instruments", r.Len())
	}
	syms := r.Symbols()
	for i := 1; i < len(syms); i++ {
		if syms[i] <= syms[i-1] {
			t.Fatal("Symbols must be sorted; iteration order must never depend on map order")
		}
	}
	if _, err := r.Get("NOPE:NOPE"); !errors.Is(err, ErrUnknownInstrument) {
		t.Fatalf("expected ErrUnknownInstrument, got %v", err)
	}
}

func TestRegistryRejectsDuplicates(t *testing.T) {
	dup := []byte(`[
	  {"symbol":"A","tickSize":"0.01"},
	  {"symbol":"A","tickSize":"0.01"}
	]`)
	if _, err := Load(dup); err == nil {
		t.Fatal("duplicate symbols must be rejected")
	}
}

func TestSpecResolveDistance(t *testing.T) {
	spec, _ := MustDefault().Get("DUKAS:EURUSD") // pipTicks 10
	ref := spec.MustPriceFromDecimal("1.08532")
	atr := core.Price(120)

	if got := spec.Resolve(core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(2)}, ref, atr); got != 20 {
		t.Fatalf("2 pips = %d ticks, want 20", got)
	}
	if got := spec.Resolve(core.Distance{Unit: core.UnitATR, Value: core.RatioFromFloat(0.5)}, ref, atr); got != 60 {
		t.Fatalf("0.5 ATR = %d ticks, want 60", got)
	}
}

// TestRegistryLoadDeterminism requires two loads to agree exactly, including
// ordering. Map iteration order is the classic source of drift here.
func TestRegistryLoadDeterminism(t *testing.T) {
	a, b := MustDefault(), MustDefault()
	sa, sb := a.Symbols(), b.Symbols()
	if len(sa) != len(sb) {
		t.Fatal("registry size differed between loads")
	}
	for i := range sa {
		if sa[i] != sb[i] {
			t.Fatalf("symbol order differed at %d: %s vs %s", i, sa[i], sb[i])
		}
		x, _ := a.Get(sa[i])
		y, _ := b.Get(sb[i])
		if x.PriceToDecimal(12345) != y.PriceToDecimal(12345) {
			t.Fatalf("%s: price rendering differed between loads", sa[i])
		}
	}
}

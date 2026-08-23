package core

import "testing"

func TestRatioMulPrice(t *testing.T) {
	cases := []struct {
		name string
		r    Ratio
		p    Price
		want Price
	}{
		{"identity", One, 12345, 12345},
		{"half rounds away from zero", One / 2, 5, 3},
		{"half rounds away from zero, negative", One / 2, -5, -3},
		{"zero ratio", 0, 999, 0},
		{"two", RatioFromInt(2), 21, 42},
		{"fractional", RatioFromFloat(0.25), 100, 25},
		{"large price", RatioFromFloat(1.5), 1_000_000_000_000, 1_500_000_000_000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.MulPrice(c.p); got != c.want {
				t.Fatalf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestRatioRoundTrip(t *testing.T) {
	for _, f := range []float64{0, 0.5, 1, 2.75, -3.125, 1e-9, 1234.5} {
		r := RatioFromFloat(f)
		if got := r.Float64(); got != f {
			t.Fatalf("round trip of %v gave %v", f, got)
		}
	}
}

func TestDivPrice(t *testing.T) {
	if got := DivPrice(300, 100); got != RatioFromInt(3) {
		t.Fatalf("300/100 = %v", got.Float64())
	}
	if got := DivPrice(1, 0); got != 0 {
		t.Fatalf("division by zero should yield 0, got %v", got)
	}
}

func TestMulDivRoundSaturates(t *testing.T) {
	// Deliberately absurd inputs: the result cannot fit, so it must saturate
	// rather than wrap or panic.
	got := mulDivRound(1<<62, 1<<62, 1)
	if got != 9223372036854775807 {
		t.Fatalf("expected saturation, got %d", got)
	}
}

func TestPriceHelpers(t *testing.T) {
	if Price(-5).Abs() != 5 || Price(5).Abs() != 5 {
		t.Fatal("Abs")
	}
	if Price(-1).Sign() != -1 || Price(0).Sign() != 0 || Price(1).Sign() != 1 {
		t.Fatal("Sign")
	}
	if Min(3, 7) != 3 || Max(3, 7) != 7 {
		t.Fatal("Min/Max")
	}
}

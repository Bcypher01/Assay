package onboarding

import "testing"

func TestDefaultIsAReproducibleMultiYearPlan(t *testing.T) {
	p := Default()
	if p.TemplateID != DefaultTemplateID || p.Symbol != DefaultSymbol || p.Timeframe != DefaultTimeframe {
		t.Fatalf("plan identity = %#v", p)
	}
	if p.ToMS-p.FromMS < int64(365*24*60*60*1000)*3 {
		t.Fatalf("plan is not multi-year: %#v", p)
	}
}

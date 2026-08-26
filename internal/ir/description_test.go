package ir

import (
	"strings"
	"testing"
)

func TestDescribeTemplateIsDerivedAndStable(t *testing.T) {
	strategy := NewMovingAverageCrossover()
	description := Describe(strategy)
	for _, want := range []string{
		"Trade DUKAS:EURUSD on the daily chart.",
		"Go long when bullish 20/50 crossover.",
		"Go short when bearish 20/50 crossover.",
		"Enter at the next bar open.",
		"Risk 0.5% per trade, with a 50 pips stop and a 100 pips target.",
	} {
		if !strings.Contains(description.Summary, want) {
			t.Fatalf("summary %q does not contain %q", description.Summary, want)
		}
	}
	if len(description.Rules) != 2 || description.Rules[0].Branch != "long" || !description.Rules[1].Enabled {
		t.Fatalf("rules = %#v", description.Rules)
	}
}

package ir

import (
	"strings"
	"testing"
)

func TestDescribeTemplateIsDerivedAndStable(t *testing.T) {
	strategy := NewMovingAverageCrossover()
	description := Describe(strategy)
	for _, want := range []string{
		"Trade EUR/USD on the daily chart.",
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

// When sizing comes from the study assumptions the document carries no risk
// percentage, but the stop and target are still executed. Leaving them out of
// the reading hides half of what the test will do.
func TestDescribeNamesStopAndTargetWithoutRiskPercent(t *testing.T) {
	strategy := NewMovingAverageCrossover()
	strategy.Risk.PerTrade = 0
	summary := Describe(strategy).Summary
	for _, want := range []string{
		"Place a 50 pips stop and a 100 pips target.",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q does not contain %q", summary, want)
		}
	}
	if strings.Contains(summary, "Risk ") {
		t.Fatalf("summary invents a risk percentage: %q", summary)
	}
}

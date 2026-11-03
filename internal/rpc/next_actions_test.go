package rpc

import (
	"strings"
	"testing"

	"github.com/bcypher01/assay/internal/analytics"
)

func TestNextActionsAskForMoreHistoryWhenTheSampleIsThin(t *testing.T) {
	small := analytics.Metrics{Trades: 8, Expectancy: analytics.Bounded{Pessimistic: 11.55, Optimistic: 49.05}}
	got := nextActions(small, nil, 3)
	if len(got) != 1 || got[0].Kind != "extend_history" {
		t.Fatalf("next actions = %#v", got)
	}
	if !strings.Contains(got[0].Reason, "8 unseen trades") {
		t.Fatalf("reason = %q", got[0].Reason)
	}

	// A one-trade sample must not be described in the plural.
	one := nextActions(analytics.Metrics{Trades: 1}, nil, 2)
	if !strings.Contains(one[0].Reason, "1 unseen trade;") {
		t.Fatalf("reason = %q", one[0].Reason)
	}

	// Without a held-out window the only fix is a longer period, even when the
	// full run traded plenty.
	unwindowed := nextActions(analytics.Metrics{Trades: 200}, nil, 0)
	if len(unwindowed) != 1 || unwindowed[0].Kind != "extend_history" {
		t.Fatalf("next actions = %#v", unwindowed)
	}
}

func TestNextActionsFollowTheCostStressResult(t *testing.T) {
	healthy := analytics.Metrics{Trades: 120, Expectancy: analytics.Bounded{Pessimistic: 5, Optimistic: 9}}
	failing := analytics.CostStressPoint{
		Multiplier: 3,
		Metrics:    analytics.Metrics{Trades: 120, Expectancy: analytics.Bounded{Pessimistic: -1, Optimistic: 3}},
	}
	stress := []analytics.CostStressPoint{{Multiplier: 1, Metrics: healthy}, failing}

	got := nextActions(healthy, stress, 4)
	if len(got) != 1 || got[0].Kind != "retest_higher_costs" {
		t.Fatalf("next actions = %#v", got)
	}

	passing := failing
	passing.Metrics.Expectancy = analytics.Bounded{Pessimistic: 2, Optimistic: 3}
	got = nextActions(healthy, []analytics.CostStressPoint{{Multiplier: 1, Metrics: healthy}, passing}, 4)
	if len(got) != 1 || got[0].Kind != "paper_trade" {
		t.Fatalf("next actions = %#v", got)
	}

	negative := analytics.Metrics{Trades: 120, Expectancy: analytics.Bounded{Pessimistic: -2, Optimistic: 3}}
	got = nextActions(negative, []analytics.CostStressPoint{{Multiplier: 1, Metrics: negative}, passing}, 4)
	if len(got) != 1 || got[0].Kind != "discard_or_rewrite" {
		t.Fatalf("next actions = %#v", got)
	}
}

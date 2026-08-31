package event

import (
	"sort"

	"github.com/bcypher01/assay/internal/core"
)

// Filter selects an event cohort. Bounds are [From, To); zero means unbounded.
// AsOf excludes records not yet known at that instant, enforcing no look-ahead.
type Filter struct {
	From          core.Time
	To            core.Time
	AsOf          core.Time
	Kinds         []Kind
	Jurisdictions []string
	Currencies    []string
	MinimumImpact Impact
}

// Query returns a new, chronological slice. Equal timestamps are ordered by ID.
func (s Snapshot) Query(f Filter) []Event {
	out := make([]Event, 0, len(s.Events))
	for _, e := range s.Events {
		if f.From != 0 && e.OccurredAt < f.From || f.To != 0 && e.OccurredAt >= f.To || f.AsOf != 0 && e.KnowledgeAt > f.AsOf {
			continue
		}
		if len(f.Kinds) > 0 && !containsKind(f.Kinds, e.Kind) ||
			len(f.Jurisdictions) > 0 && !containsString(f.Jurisdictions, e.Jurisdiction) ||
			len(f.Currencies) > 0 && !overlaps(f.Currencies, e.Currencies) ||
			f.MinimumImpact != "" && impactRank(e.Impact) < impactRank(f.MinimumImpact) {
			continue
		}
		out = append(out, cloneEvent(e))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OccurredAt != out[j].OccurredAt {
			return out[i].OccurredAt < out[j].OccurredAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func containsKind(a []Kind, v Kind) bool {
	for _, x := range a {
		if x == v {
			return true
		}
	}
	return false
}
func containsString(a []string, v string) bool {
	for _, x := range a {
		if x == v {
			return true
		}
	}
	return false
}
func overlaps(a, b []string) bool {
	for _, x := range a {
		if containsString(b, x) {
			return true
		}
	}
	return false
}
func impactRank(v Impact) int {
	if v == ImpactHigh {
		return 3
	}
	if v == ImpactMedium {
		return 2
	}
	if v == ImpactLow {
		return 1
	}
	return 0
}

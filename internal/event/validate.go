package event

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var ErrInvalidEvent = errors.New("event: invalid event")

var decimalPattern = regexp.MustCompile(`^[+-]?[0-9]+(?:\.[0-9]+)?$`)

// Validate checks the integrity conditions required before an event can enter
// an immutable snapshot. It does not rewrite values: a source must be explicit
// about timestamp precision and currency relevance.
func (e Event) Validate() error {
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.Title) == "" {
		return invalid("id and title are required")
	}
	if !IsKnownKind(e.Kind) {
		return invalid("unknown kind %q", e.Kind)
	}
	if e.Precision != PrecisionExact && e.Precision != PrecisionDay && e.Precision != PrecisionMonth {
		return invalid("invalid precision %q", e.Precision)
	}
	if e.OccurredAt <= 0 || e.KnowledgeAt <= 0 {
		return invalid("occurredAt and knowledgeAt are required")
	}
	if e.Precision == PrecisionDay && !isUTCMidnight(e.OccurredAt) {
		return invalid("day-precision occurredAt must be UTC midnight")
	}
	if e.Precision == PrecisionMonth && (!isUTCMidnight(e.OccurredAt) || e.OccurredAt.Std().Day() != 1) {
		return invalid("month-precision occurredAt must be the first UTC day")
	}
	if e.Impact != ImpactLow && e.Impact != ImpactMedium && e.Impact != ImpactHigh {
		return invalid("invalid impact %q", e.Impact)
	}
	if e.Status != StatusScheduled && e.Status != StatusReleased && e.Status != StatusRevised && e.Status != StatusEstimated {
		return invalid("invalid status %q", e.Status)
	}
	if e.Status == StatusRevised && strings.TrimSpace(e.RevisionOf) == "" {
		return invalid("revised records require revisionOf")
	}
	if e.Status != StatusScheduled && e.KnowledgeAt < e.OccurredAt {
		return invalid("released records cannot be known before occurrence")
	}
	if strings.TrimSpace(e.Jurisdiction) == "" {
		return invalid("jurisdiction is required")
	}
	if len(e.Currencies) == 0 {
		return invalid("at least one currency is required")
	}
	if !sort.StringsAreSorted(e.Currencies) {
		return invalid("currencies must be sorted")
	}
	for i, c := range e.Currencies {
		if len(c) != 3 || c != strings.ToUpper(c) || (i > 0 && e.Currencies[i-1] == c) {
			return invalid("currencies must be unique uppercase ISO-like codes")
		}
	}
	if strings.TrimSpace(e.Source.Provider) == "" || strings.TrimSpace(e.Source.RecordID) == "" {
		return invalid("source provider and recordId are required")
	}
	for _, d := range []*Decimal{e.Actual, e.Forecast, e.Previous} {
		if d != nil && !decimalPattern.MatchString(string(*d)) {
			return invalid("malformed decimal %q", *d)
		}
	}
	return nil
}

func isUTCMidnight(t interface{ Std() time.Time }) bool {
	v := t.Std()
	return v.Hour() == 0 && v.Minute() == 0 && v.Second() == 0 && v.Nanosecond() == 0
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidEvent}, args...)...)
}

package ir

import (
	"fmt"
	"strings"

	"github.com/Bcypher01/Assay/internal/core"
)

// Description is the user-facing reading of a strategy document. It is
// generated from the persisted rule structure, never accepted as an input.
type Description struct {
	Summary string
	Rules   []RuleDescription
}

type RuleDescription struct {
	Branch  string
	Text    string
	Enabled bool
}

// Describe returns stable language for the normal builder and template flows.
// Branch names are part of v2's named rule model; timing, market, timeframe,
// and risk are derived directly from executable fields.
func Describe(s *Strategy) Description {
	if s == nil {
		return Description{}
	}
	if s.Schema == SchemaV1 {
		migrated, err := s.MigrateV1ToV2()
		if err != nil {
			return Description{}
		}
		s = migrated
	}
	market := strings.Join(s.Universe.Symbols, ", ")
	tf := timeframeName(s.Timeframes["entry"])
	parts := []string{}
	if market != "" && tf != "" {
		parts = append(parts, fmt.Sprintf("Trade %s on the %s chart.", market, tf))
	}
	rules := make([]RuleDescription, 0, 2)
	for _, branch := range []struct {
		side  string
		entry EntryBranch
	}{{"long", s.Entries.Long}, {"short", s.Entries.Short}} {
		if branch.entry.Name == "" && !branch.entry.Enabled {
			continue
		}
		text := branch.entry.Name
		if text == "" {
			text = "No rule configured"
		}
		rules = append(rules, RuleDescription{Branch: branch.side, Text: text, Enabled: branch.entry.Enabled})
		if branch.entry.Enabled {
			parts = append(parts, fmt.Sprintf("Go %s when %s.", branch.side, lowerFirst(text)))
		}
	}
	if s.Entries.Timing != "" {
		parts = append(parts, "Enter at the "+entryTimingName(s.Entries.Timing)+".")
	}
	if s.Risk.PerTrade > 0 {
		parts = append(parts, fmt.Sprintf("Risk %g%% per trade", s.Risk.PerTrade.Float64()*100)+riskSuffix(s.Risk))
	}
	return Description{Summary: strings.Join(parts, " "), Rules: rules}
}

func timeframeName(tf core.TF) string {
	if !tf.Valid() {
		return ""
	}
	switch tf {
	case core.TFM1:
		return "1-minute"
	case core.TFM5:
		return "5-minute"
	case core.TFM15:
		return "15-minute"
	case core.TFH1:
		return "1-hour"
	case core.TFH4:
		return "4-hour"
	case core.TFD1:
		return "daily"
	default:
		return strings.ToLower(tf.String())
	}
}

func entryTimingName(t EntryTiming) string {
	if t == EntryTimingNextOpen {
		return "next bar open"
	}
	return string(t)
}

func riskSuffix(r Risk) string {
	parts := []string{}
	if r.Stop.Value > 0 {
		parts = append(parts, "a "+humanDistance(r.Stop)+" stop")
	}
	if r.Target.Value > 0 {
		parts = append(parts, "a "+humanDistance(r.Target)+" target")
	}
	if len(parts) == 0 {
		return "."
	}
	return ", with " + strings.Join(parts, " and ") + "."
}

func humanDistance(d core.Distance) string {
	return fmt.Sprintf("%g %s", d.Value.Float64(), d.Unit)
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

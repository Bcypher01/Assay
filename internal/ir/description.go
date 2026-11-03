package ir

import (
	"fmt"
	"strings"

	"github.com/bcypher01/assay/internal/core"
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
	market := strings.Join(displaySymbols(s.Universe.Symbols), ", ")
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
	} else if clause := distanceClause(s.Risk); clause != "" {
		// Sizing can come from the study assumptions instead of the document.
		// The stop and target are still part of what gets tested, so the
		// reading must name them either way.
		parts = append(parts, "Place "+clause+".")
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
	if clause := distanceClause(r); clause != "" {
		return ", with " + clause + "."
	}
	return "."
}

// distanceClause names the stop and target without deciding how the sentence
// around them begins.
func distanceClause(r Risk) string {
	parts := []string{}
	if r.Stop.Value > 0 {
		parts = append(parts, "a "+humanDistance(r.Stop)+" stop")
	}
	if r.Target.Value > 0 {
		parts = append(parts, "a "+humanDistance(r.Target)+" target")
	}
	return strings.Join(parts, " and ")
}

// displaySymbols writes instruments the way the interface does: without the
// venue prefix, and as a pair for six-letter FX codes. The stored symbol keeps
// its venue; only the reading is softened.
func displaySymbols(symbols []string) []string {
	out := make([]string, len(symbols))
	for i, symbol := range symbols {
		if cut := strings.LastIndex(symbol, ":"); cut >= 0 {
			symbol = symbol[cut+1:]
		}
		if len(symbol) == 6 && symbol == strings.ToUpper(symbol) {
			symbol = symbol[:3] + "/" + symbol[3:]
		}
		out[i] = symbol
	}
	return out
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

// Package onboarding defines the small, deterministic first-run path exposed
// to local clients. It contains no user state: completion belongs in the UI.
package onboarding

import (
	"encoding/json"
	"time"
)

const (
	// DefaultTemplateID is intentionally a daily strategy. It can be prepared
	// from the bundled public FX fallback without requiring intraday history.
	DefaultTemplateID = "moving-average-crossover"
	DefaultSymbol     = "DUKAS:EURUSD"
	DefaultTimeframe  = "d1"
)

// Plan tells a client which worked strategy and data window to open first.
// Milliseconds are used to match the RPC data APIs exactly.
type Plan struct {
	TemplateID  string `json:"template_id"`
	Symbol      string `json:"symbol"`
	Timeframe   string `json:"timeframe"`
	FromMS      int64  `json:"from_ms"`
	ToMS        int64  `json:"to_ms"`
	Title       string `json:"title"`
	Explanation string `json:"explanation"`

	// Strategy is the editable document the client opens first. It is embedded
	// rather than left to be fetched separately so a static UI can start a real
	// study from this one response; without it the client has a name and a
	// window but nothing executable to run.
	Strategy json.RawMessage `json:"strategy,omitempty"`
}

// Default is a fixed four-year historical window. Fixing the window makes the
// example reproducible and ensures the first successful study is multi-year.
func Default() Plan {
	return Plan{
		TemplateID: DefaultTemplateID,
		Symbol:     DefaultSymbol, Timeframe: DefaultTimeframe,
		FromMS:      time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
		ToMS:        time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
		Title:       "Run a worked four-year example",
		Explanation: "Start with the editable 20/50 moving-average crossover on EUR/USD daily bars. Data preparation happens before the study; its built-in execution costs remain enabled.",
	}
}

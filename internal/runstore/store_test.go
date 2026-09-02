package runstore

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bcypher01/assay/internal/analytics"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/ir"
	"github.com/bcypher01/assay/internal/sim"
)

func testStudy(stop core.Price) *analytics.Study {
	return &analytics.Study{Strategy: &ir.Strategy{Name: "example", Universe: ir.Universe{Symbols: []string{"EURUSD"}}, Risk: ir.Risk{Stop: core.Distance{Value: core.Ratio(stop)}}}, Snapshot: "snapshot-a", Scope: analytics.Scope{Period: "2025"}}
}
func testResult(equity float64) *analytics.StudyResult {
	return &analytics.StudyResult{OutOfSample: analytics.Metrics{Trades: 2, FinalEquity: analytics.Bounded{Pessimistic: equity, Optimistic: equity + 1}, Expectancy: analytics.Bounded{Pessimistic: equity / 2, Optimistic: equity}}}
}
func TestTrialsPersistAndRetentionKeepsIndex(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir(), WithMaxBytes(1))
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Put(ctx, testStudy(10), testResult(4))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(ctx, testStudy(10), testResult(4)); err != nil {
		t.Fatal(err)
	}
	b, err := s.Put(ctx, testStudy(11), testResult(8))
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("changed strategy has same run ID")
	}
	if err := s.PutTrades(ctx, a, []sim.Trade{{Side: core.SideLong, EntryTime: 1, Pessimistic: sim.Outcome{Net: 5}}}); err != nil {
		t.Fatal(err)
	}
	n, err := s.Trials(ctx, Scope{Instruments: []string{"EURUSD"}, Period: "2025"})
	if err != nil || n != 2 {
		t.Fatalf("trials = %d, %v", n, err)
	}
	// Reopening must use the persisted database rather than a process counter.
	root := s.root
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(root, WithMaxBytes(1))
	if err != nil {
		t.Fatal(err)
	}
	n, err = s.Trials(ctx, Scope{Instruments: []string{"EURUSD"}, Period: "2025"})
	if err != nil || n != 2 {
		t.Fatalf("reopened trials = %d, %v", n, err)
	}
	r, err := s.Get(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if r.TradeListPresent {
		t.Fatal("retention should evict trade file")
	}
}

func TestOpenMigratesLegacyJSONIndexIntoSQLite(t *testing.T) {
	root := t.TempDir()
	legacy := RunSummary{
		ID:           "legacy-run",
		StrategyHash: "strategy",
		Snapshot:     "snapshot",
		Name:         "Legacy run",
		Scope:        Scope{Instruments: []string{"EURUSD"}, Period: "2024"},
		CreatedAt:    time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		Trades:       3,
		FinalEquity:  analytics.Bounded{Pessimistic: 9_900, Optimistic: 10_100},
	}
	if err := writeJSON(filepath.Join(root, "index.json"), []RunSummary{legacy}); err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(root, "runs", string(legacy.ID))
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(legacyDir, "study.json"), testStudy(10)); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(legacyDir, "result.json"), testResult(legacy.FinalEquity.Pessimistic)); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items, err := s.List(context.Background(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != legacy.ID || items[0].FinalEquity != legacy.FinalEquity {
		t.Fatalf("migrated runs = %#v", items)
	}
	if _, err := os.Stat(filepath.Join(root, "history.db")); err != nil {
		t.Fatalf("SQLite history database was not created: %v", err)
	}
	if _, err := s.Get(context.Background(), legacy.ID); err != nil {
		t.Fatalf("SQLite-migrated run cannot be loaded: %v", err)
	}
}

func TestComparisonAndExportAreDeterministic(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Put(ctx, testStudy(10), testResult(4))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Put(ctx, testStudy(11), testResult(8))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Compare(ctx, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.ParamChanges) != 1 || d.ParamChanges[0].Path != "risk.stop" {
		t.Fatalf("risk change is not represented in diff: %#v", d.ParamChanges)
	}
	if got := d.MetricDeltas["outOfSample.finalEquity"].Pessimistic; got != 4 {
		t.Fatalf("equity delta = %v", got)
	}
	var out bytes.Buffer
	if err := s.Export(ctx, a, &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 {
		t.Fatal("empty export")
	}
}

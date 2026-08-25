package runstore

import (
	"bytes"
	"context"
	"testing"

	"github.com/Bcypher01/Assay/internal/analytics"
	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/ir"
	"github.com/Bcypher01/Assay/internal/sim"
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
	// Reopening must use the persisted index rather than a process counter.
	s, err = Open(s.root, WithMaxBytes(1))
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

package primitive

import (
	"context"
	"errors"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Bcypher01/Assay/internal/core"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
)

type probePrimitive struct {
	calls *int
	mu    *sync.Mutex
}

func (p probePrimitive) Spec() Spec {
	return Spec{Pack: "test", Type: "probe", Out: KindBool, Params: (*assayv1.SampleParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (p probePrimitive) New(proto.Message, core.TF) (Instance, error) {
	return probeInstance(p), nil
}

type probeInstance struct {
	calls *int
	mu    *sync.Mutex
}

func (p probeInstance) Eval(_ *EvalCtx, s *core.Series) (Value, error) {
	p.mu.Lock()
	*p.calls++
	p.mu.Unlock()
	return Value{Kind: KindBool, Bool: core.NewBitset(s.Len())}, nil
}
func (probeInstance) Overlays(core.Time, core.Time) []Overlay { return nil }

func series() *core.Series {
	s := core.NewSeries("X", core.TFM1, 2)
	s.Append(0, 1, 1, 1, 1, 0)
	s.Append(60_000, 1, 1, 1, 1, 0)
	return s
}

func TestRegistryRejectsDuplicateAndListsStably(t *testing.T) {
	r := NewRegistry()
	var calls int
	var mu sync.Mutex
	p := probePrimitive{&calls, &mu}
	if err := r.Register(p); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(p); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate error=%v", err)
	}
	if _, err := r.Get("test", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not found=%v", err)
	}
	got := r.Specs()
	if len(got) != 1 || got[0].Type != "probe" {
		t.Fatalf("specs=%#v", got)
	}
}

func TestHashConfigCanonicalAndSnapshotBound(t *testing.T) {
	a := &assayv1.SampleParams{Period: 14, Enabled: true}
	b := &assayv1.SampleParams{Enabled: true, Period: 14}
	ha, err := HashConfig("indicator", "sample", a, core.TFM15, "snapshot-a")
	if err != nil {
		t.Fatal(err)
	}
	hb, _ := HashConfig("indicator", "sample", b, core.TFM15, "snapshot-a")
	hc, _ := HashConfig("indicator", "sample", b, core.TFM15, "snapshot-b")
	if ha != hb {
		t.Fatal("equivalent params produced distinct hash")
	}
	if ha == hc {
		t.Fatal("snapshot was omitted from hash")
	}
}

func TestCacheCoalescesConcurrentConfig(t *testing.T) {
	c := NewMemoCache(1)
	var calls int
	var mu sync.Mutex
	p := probePrimitive{&calls, &mu}
	params := &assayv1.SampleParams{Period: 7}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := Evaluate(context.Background(), p, params, core.TFM1, series(), "x", c); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("evaluations=%d, want one", calls)
	}
	hits, misses := c.Stats()
	if hits != 11 || misses != 1 {
		t.Fatalf("stats=(%d,%d)", hits, misses)
	}
	other, _ := HashConfig("test", "other", params, core.TFM1, "x")
	if _, err := c.GetOrEval(other, func() (Value, error) { return Value{Kind: KindBool, Bool: core.NewBitset(0)}, nil }); err != nil {
		t.Fatal(err)
	}
	if c.Len() != 1 {
		t.Fatal("cache bound not honoured")
	}
}

func TestCompletedSeriesHidesPartialHigherTimeframeBar(t *testing.T) {
	s := core.NewSeries("X", core.TFM1, 241)
	for i := 0; i < 241; i++ {
		t := core.Time(int64(i) * core.TFM1.Millis())
		s.Append(t, core.Price(i), core.Price(i+1), core.Price(i-1), core.Price(i), 1)
	}
	got, err := CompletedSeries(s, core.TFH4)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != 1 {
		t.Fatalf("completed H4 bars=%d, want 1", got.Len())
	}
	if got.Time[0] != 0 || got.Open[0] != 0 || got.Close[0] != 239 || got.High[0] != 240 || got.Low[0] != -1 {
		t.Fatalf("aggregate=%#v", got)
	}
	var calls int
	var mu sync.Mutex
	if _, _, err := Evaluate(context.Background(), probePrimitive{&calls, &mu}, &assayv1.SampleParams{}, core.TFH4, s, "x", nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

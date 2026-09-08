package smc

import (
	"context"
	"testing"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
)

// BenchmarkFairValueGap2p6M is the pack's hot-path guardrail. The generated
// series deliberately has no gaps, so it measures the ordinary scan rather
// than allocating an overlay for every bar.
func BenchmarkFairValueGap2p6M(b *testing.B) {
	const n = 2_600_000
	s := core.NewSeries("X", core.TFM1, n)
	for i := 0; i < n; i++ {
		p := core.Price(1000 + i%3)
		s.Append(core.Time(i*60_000), p, p+2, p-2, p, 1)
	}
	p := FairValueGap{}
	params := &assayv1.FairValueGapParams{MinSizeTicks: 10, ExpiryBars: 100, MitigationRule: assayv1.MitigationRule_MITIGATION_RULE_FULL_FILL}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := primitive.Evaluate(context.Background(), p, params, core.TFM1, s, "benchmark", nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFairValueGapSweep models the 1000-config memoised sweep from
// docs/architecture.md: a bounded LRU plus a fresh cold pass per config.
// Eviction must recycle the 119 MiB Zone array into primitive's pool so that
// later configs do not reallocate it.
func BenchmarkFairValueGapSweep(b *testing.B) {
	const n = 2_600_000
	s := core.NewSeries("X", core.TFM1, n)
	for i := 0; i < n; i++ {
		p := core.Price(1000 + i%3)
		s.Append(core.Time(i*60_000), p, p+2, p-2, p, 1)
	}
	p := FairValueGap{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache := primitive.NewMemoCache(32)
		for c := 0; c < 1000; c++ {
			params := &assayv1.FairValueGapParams{MinSizeTicks: uint32(10 + c%90), ExpiryBars: 100, MitigationRule: assayv1.MitigationRule_MITIGATION_RULE_FULL_FILL}
			if _, _, err := primitive.Evaluate(context.Background(), p, params, core.TFM1, s, "benchmark", cache); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkFairValueGapCacheHit reports the cost of a memoised hit so the
// sweep figure can be read against it: 1000 configs x cold pass + hit cost.
func BenchmarkFairValueGapCacheHit(b *testing.B) {
	const n = 2_600_000
	s := core.NewSeries("X", core.TFM1, n)
	for i := 0; i < n; i++ {
		p := core.Price(1000 + i%3)
		s.Append(core.Time(i*60_000), p, p+2, p-2, p, 1)
	}
	p := FairValueGap{}
	params := &assayv1.FairValueGapParams{MinSizeTicks: 10, ExpiryBars: 100, MitigationRule: assayv1.MitigationRule_MITIGATION_RULE_FULL_FILL}
	cache := primitive.NewMemoCache(1)
	if _, _, err := primitive.Evaluate(context.Background(), p, params, core.TFM1, s, "benchmark", cache); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := primitive.Evaluate(context.Background(), p, params, core.TFM1, s, "benchmark", cache); err != nil {
			b.Fatal(err)
		}
	}
}

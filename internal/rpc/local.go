package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/engine"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/ir"
	"github.com/Bcypher01/Assay/internal/sim"
	"github.com/Bcypher01/Assay/internal/store"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
)

// NewLocalServer wires the local bar cache to the engine. v0 deliberately
// supports one instrument per run: portfolio execution needs an engine-level
// alignment model rather than silently running only the first symbol.
func NewLocalServer(cache *store.LocalStore, registry *instrument.Registry) (*Server, error) {
	if cache == nil || registry == nil {
		return nil, fmt.Errorf("rpc: local cache and instrument registry are required")
	}
	instruments := make([]*assayv1.Instrument, 0, registry.Len())
	for _, symbol := range registry.Symbols() {
		spec, err := registry.Get(symbol)
		if err != nil {
			return nil, err
		}
		instruments = append(instruments, &assayv1.Instrument{Symbol: spec.Symbol, QualityTier: string(spec.Quality), Timeframes: supportedTimeframes(spec.Quality)})
	}
	s := &Server{Instruments: instruments}
	s.RunFn = func(ctx context.Context, req *assayv1.RunRequest, send func(*assayv1.RunUpdate) error) (*assayv1.RunSummary, error) {
		return runLocal(ctx, cache, registry, req, send)
	}
	return s, nil
}

func runLocal(ctx context.Context, cache *store.LocalStore, registry *instrument.Registry, req *assayv1.RunRequest, send func(*assayv1.RunUpdate) error) (*assayv1.RunSummary, error) {
	var strategy ir.Strategy
	if err := json.Unmarshal(req.StrategyJson, &strategy); err != nil {
		return nil, fmt.Errorf("rpc: decode strategy: %w", err)
	}
	if len(strategy.Universe.Symbols) != 1 {
		return nil, fmt.Errorf("rpc: v0 supports exactly one instrument per run")
	}
	if req.FromMs >= req.ToMs {
		return nil, fmt.Errorf("rpc: run window must have from_ms before to_ms")
	}
	tf, ok := strategy.Timeframes["entry"]
	if !ok || !tf.Valid() {
		return nil, fmt.Errorf("rpc: strategy entry timeframe is required")
	}
	symbol := strategy.Universe.Symbols[0]
	spec, err := registry.Get(symbol)
	if err != nil {
		return nil, err
	}
	if err := send(progressUpdate(15, "loading data")); err != nil {
		return nil, err
	}
	series, manifests, err := cache.Load(ctx, symbol, tf, core.Time(req.FromMs), core.Time(req.ToMs))
	if err != nil {
		return nil, fmt.Errorf("rpc: load %s %s: %w", symbol, tf, err)
	}
	if len(manifests) == 0 || series.Len() == 0 {
		return nil, fmt.Errorf("rpc: no cached bars for requested window")
	}
	if err := send(progressUpdate(40, "evaluating strategy")); err != nil {
		return nil, err
	}
	config := sim.Config{Spec: *spec, Risk: sim.RiskModel{InitialEquity: 100_000, FixedLots: core.One}}
	result, _, err := engine.Run(ctx, &strategy, engine.Data{Series: series, Config: config, Snapshot: req.Snapshot})
	if err != nil {
		return nil, err
	}
	if err := send(progressUpdate(80, "streaming results")); err != nil {
		return nil, err
	}
	const chunk = 2048
	for from := 0; from < len(result.Equity); from += chunk {
		to := from + chunk
		if to > len(result.Equity) {
			to = len(result.Equity)
		}
		values := make([]int64, to-from)
		for i := from; i < to; i++ {
			values[i-from] = int64(result.Equity[i])
		}
		if err := send(&assayv1.RunUpdate{Payload: &assayv1.RunUpdate_Equity{Equity: &assayv1.EquityChunk{Values: values}}}); err != nil {
			return nil, err
		}
	}
	if err := send(&assayv1.RunUpdate{Payload: &assayv1.RunUpdate_Trades{Trades: &assayv1.TradeChunk{Count: uint32(len(result.Trades))}}}); err != nil {
		return nil, err
	}
	return &assayv1.RunSummary{Trades: uint32(len(result.Trades)), Unresolved: uint32(result.Unresolved)}, nil
}

func supportedTimeframes(q instrument.QualityTier) []string {
	all := []core.TF{core.TFM1, core.TFM5, core.TFM15, core.TFM30, core.TFH1, core.TFH4, core.TFD1, core.TFW1}
	out := make([]string, 0, len(all))
	for _, tf := range all {
		if q.Supports(tf) {
			out = append(out, tf.String())
		}
	}
	return out
}

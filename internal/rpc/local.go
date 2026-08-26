package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/Bcypher01/Assay/internal/analytics"
	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/engine"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/ir"
	"github.com/Bcypher01/Assay/internal/library"
	"github.com/Bcypher01/Assay/internal/runstore"
	"github.com/Bcypher01/Assay/internal/sim"
	"github.com/Bcypher01/Assay/internal/store"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
)

// NewLocalServer wires the local bar cache to the engine. v0 deliberately
// supports one instrument per run: portfolio execution needs an engine-level
// alignment model rather than silently running only the first symbol.
func NewLocalServer(cache *store.LocalStore, registry *instrument.Registry) (*Server, error) {
	return NewLocalServerWithServices(cache, registry, nil, nil)
}

// NewLocalServerWithServices wires the client-facing local server. Loader is
// optional for fixture and offline use; when present it prepares missing data
// before a run. History is optional for callers that do not persist studies.
func NewLocalServerWithServices(cache *store.LocalStore, registry *instrument.Registry, loader *library.Loader, history runstore.Store) (*Server, error) {
	if cache == nil || registry == nil {
		return nil, fmt.Errorf("rpc: local cache and instrument registry are required")
	}
	instruments := instrumentsFromRegistry(registry)
	s := &Server{Instruments: instruments, RunStore: history}
	if loader != nil {
		loader.Registry = registry
		s.DataStatusFn = func(ctx context.Context, req *assayv1.EnsureDataRequest) (*assayv1.DataStatusResponse, error) {
			r, err := dataRequest(loader, req)
			if err != nil {
				return nil, err
			}
			return dataStatusResponse(loader.Status(ctx, r)), nil
		}
		s.EnsureDataFn = func(ctx context.Context, req *assayv1.EnsureDataRequest, report func(*assayv1.Progress) error) error {
			request, err := dataRequest(loader, req)
			if err != nil {
				return err
			}
			return loader.Ensure(ctx, request, func(p library.Progress) {
				_ = report(&assayv1.Progress{Percent: p.Percent, Stage: p.Stage, BytesDone: p.BytesDone, BytesTotal: p.BytesTotal, CurrentFile: p.CurrentFile, StageCode: progressStageCode(p.Stage)})
			})
		}
	}
	s.RunFn = func(ctx context.Context, req *assayv1.RunRequest, send func(*assayv1.RunUpdate) error) (*assayv1.RunSummary, error) {
		if loader != nil {
			var strategy ir.Strategy
			if err := json.Unmarshal(req.StrategyJson, &strategy); err != nil {
				return nil, fmt.Errorf("rpc: decode strategy: %w", err)
			}
			tf, ok := strategy.Timeframes["entry"]
			if !ok {
				return nil, fmt.Errorf("rpc: strategy entry timeframe is required")
			}
			r, err := loader.DeriveRequest(strategy.Universe.Symbols, []core.TF{tf}, core.Time(req.FromMs), core.Time(req.ToMs))
			if err != nil {
				return nil, err
			}
			status := loader.Status(ctx, r)
			if status.Readiness != library.ReadinessReady {
				return &assayv1.RunSummary{
					Error:     dataStatusMessage(status),
					ErrorCode: status.ErrorCode,
				}, nil
			}
		}
		return runLocal(ctx, cache, registry, history, req, send)
	}
	return s, nil
}

func dataRequest(loader *library.Loader, req *assayv1.EnsureDataRequest) (library.Request, error) {
	if req.FromMs >= req.ToMs {
		return library.Request{}, fmt.Errorf("rpc: data window must have from_ms before to_ms")
	}
	tf, err := core.ParseTF(req.Timeframe)
	if err != nil {
		return library.Request{}, err
	}
	return loader.DeriveRequest([]string{req.Symbol}, []core.TF{tf}, core.Time(req.FromMs), core.Time(req.ToMs))
}

func dataStatusResponse(status library.Status) *assayv1.DataStatusResponse {
	out := &assayv1.DataStatusResponse{
		Readiness:                   dataReadiness(status.Readiness),
		RequestedFromMs:             int64(status.RequestedFrom),
		RequestedToMs:               int64(status.RequestedTo),
		CachedFromMs:                int64(status.CachedFrom),
		CachedToMs:                  int64(status.CachedTo),
		Snapshot:                    status.Snapshot,
		Sources:                     append([]string(nil), status.Sources...),
		LastSuccessfulPreparationMs: int64(status.LastSuccessfulPrep),
		ExternalAccessRequired:      status.ExternalRequired,
		ErrorCode:                   status.ErrorCode,
	}
	if status.Err != nil {
		out.Error = status.Err.Error()
	}
	for _, p := range status.MissingPartitions {
		out.MissingPartitions = append(out.MissingPartitions, &assayv1.DataPartition{
			Symbol:    p.Symbol,
			Timeframe: p.TF.String(),
			Month:     p.Month,
			FromMs:    int64(p.From),
			ToMs:      int64(p.To),
		})
	}
	return out
}

func dataReadiness(readiness library.Readiness) assayv1.DataReadiness {
	switch readiness {
	case library.ReadinessReady:
		return assayv1.DataReadiness_DATA_READINESS_READY
	case library.ReadinessPartial:
		return assayv1.DataReadiness_DATA_READINESS_PARTIAL
	case library.ReadinessMissing:
		return assayv1.DataReadiness_DATA_READINESS_MISSING
	case library.ReadinessSourceUnavailable:
		return assayv1.DataReadiness_DATA_READINESS_SOURCE_UNAVAILABLE
	case library.ReadinessCorrupt:
		return assayv1.DataReadiness_DATA_READINESS_CORRUPT
	case library.ReadinessUnsupported:
		return assayv1.DataReadiness_DATA_READINESS_UNSUPPORTED
	default:
		return assayv1.DataReadiness_DATA_READINESS_UNSPECIFIED
	}
}

func dataStatusMessage(status library.Status) string {
	if status.Err != nil {
		return status.Err.Error()
	}
	switch status.Readiness {
	case library.ReadinessPartial:
		return "Data is only partially prepared for the requested market, timeframe, and date window."
	case library.ReadinessUnsupported:
		return "The requested timeframe is not supported for this instrument."
	case library.ReadinessCorrupt:
		return "Cached data failed verification and must be prepared again."
	default:
		return "Data must be prepared locally before this backtest can run."
	}
}

func runLocal(ctx context.Context, cache *store.LocalStore, registry *instrument.Registry, history runstore.Store, req *assayv1.RunRequest, send func(*assayv1.RunUpdate) error) (*assayv1.RunSummary, error) {
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
	config, err := runConfig(*spec, &strategy, req.Assumptions)
	if err != nil {
		return nil, err
	}
	snapshot := store.SnapshotHash(manifests)
	result, nearMiss, err := engine.Run(ctx, &strategy, engine.Data{Series: series, Config: config, Snapshot: snapshot})
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
	returns := analytics.Returns{Pessimistic: make([]float64, len(result.Trades)), Optimistic: make([]float64, len(result.Trades))}
	summary := &assayv1.RunSummary{Trades: uint32(len(result.Trades)), Unresolved: uint32(result.Unresolved)}
	var netPnL core.Price
	for i, trade := range result.Trades {
		returns.Pessimistic[i] = float64(trade.Pessimistic.Net)
		returns.Optimistic[i] = float64(trade.Optimistic.Net)
		netPnL += trade.Pessimistic.Net
		if trade.Side == core.SideShort {
			summary.ShortTrades++
		} else {
			summary.LongTrades++
		}
	}
	metrics, err := analytics.MetricsFor(returns)
	if err != nil {
		return nil, err
	}
	summary.Wins = uint32(metrics.Wins)
	summary.Losses = uint32(metrics.Losses)
	summary.NetPnl = int64(netPnL)
	summary.Expectancy = metrics.Expectancy.Pessimistic
	summary.ProfitFactor = metrics.ProfitFactor.Pessimistic
	if history != nil {
		study := &analytics.Study{Strategy: &strategy, Snapshot: snapshot, Scope: analytics.Scope{Instruments: append([]string(nil), strategy.Universe.Symbols...), Period: strconv.FormatInt(req.FromMs, 10) + "-" + strconv.FormatInt(req.ToMs, 10)}, Assumptions: effectiveAssumptions(config)}
		nearMisses := make([]analytics.NearMiss, 0, len(nearMiss.SlotFailures))
		for _, condition := range []string{"bias", "poi", "trigger"} {
			if count := nearMiss.SlotFailures[condition]; count > 0 {
				nearMisses = append(nearMisses, analytics.NearMiss{Condition: condition, Count: count})
			}
		}
		id, err := history.Put(ctx, study, &analytics.StudyResult{OutOfSample: metrics, InSample: metrics, NearMisses: nearMisses, Unresolved: result.Unresolved})
		if err != nil {
			return nil, err
		}
		if trades, ok := history.(interface {
			PutTrades(context.Context, runstore.RunID, []sim.Trade) error
		}); ok {
			if err := trades.PutTrades(ctx, id, result.Trades); err != nil {
				return nil, err
			}
		}
		summary.RunId = string(id)
	}
	return summary, nil
}

func effectiveAssumptions(cfg sim.Config) analytics.Assumptions {
	a := analytics.Assumptions{InitialEquity: int64(cfg.Risk.InitialEquity), SpreadTicks: cfg.Spec.Costs.SpreadTicks, CommissionFlat: cfg.Spec.Costs.CommissionFlat, FixedLots: cfg.Risk.FixedLots.Float64(), FillPolicy: "next_open"}
	if cfg.Risk.RiskPerTrade > 0 {
		a.RiskPerTrade = cfg.Risk.RiskPerTrade.Float64()
	}
	if cfg.Spec.Costs.CommissionMode == instrument.CommissionBps {
		a.CommissionBps = cfg.Spec.Costs.CommissionBps.Float64() * 10000
	}
	if cfg.Slippage.Enabled {
		a.SlippageTicks = int64(cfg.Slippage.Ticks)
	} else {
		a.SlippageTicks = cfg.Spec.Costs.SlippageTicks
	}
	return a
}

// runConfig turns the explicit, persisted study assumptions into simulator
// configuration. Zero overrides intentionally retain the instrument's
// non-zero defaults; a client therefore cannot accidentally obtain a free-fill
// backtest by omitting execution fields.
func runConfig(spec instrument.Spec, strategy *ir.Strategy, a *assayv1.StudyAssumptions) (sim.Config, error) {
	cfg := sim.Config{Spec: spec, Risk: sim.RiskModel{InitialEquity: 100_000, FixedLots: core.One}}
	if strategy != nil && strategy.Risk.PerTrade > 0 {
		cfg.Risk.RiskPerTrade = strategy.Risk.PerTrade
	}
	if a == nil {
		return cfg, nil
	}
	if a.InitialEquity <= 0 {
		return sim.Config{}, fmt.Errorf("rpc: assumptions.initial_equity must be positive")
	}
	if a.RiskPerTrade < 0 || a.RiskPerTrade > 1 || math.IsNaN(a.RiskPerTrade) || math.IsInf(a.RiskPerTrade, 0) {
		return sim.Config{}, fmt.Errorf("rpc: assumptions.risk_per_trade must be between 0 and 1")
	}
	if a.FixedLots < 0 || math.IsNaN(a.FixedLots) || math.IsInf(a.FixedLots, 0) {
		return sim.Config{}, fmt.Errorf("rpc: assumptions.fixed_lots must not be negative")
	}
	if a.SpreadTicks < 0 || a.CommissionBps < 0 || a.CommissionFlat < 0 || a.SlippageTicks < 0 {
		return sim.Config{}, fmt.Errorf("rpc: execution-cost assumptions must not be negative")
	}
	if a.FillPolicy != "" && a.FillPolicy != "next_open" {
		return sim.Config{}, fmt.Errorf("rpc: assumptions.fill_policy only supports next_open")
	}
	cfg.Risk.InitialEquity = core.Price(a.InitialEquity)
	if a.RiskPerTrade > 0 {
		cfg.Risk.RiskPerTrade = core.RatioFromFloat(a.RiskPerTrade)
		cfg.Risk.FixedLots = 0
	} else if a.FixedLots > 0 {
		cfg.Risk.RiskPerTrade = 0
		cfg.Risk.FixedLots = core.RatioFromFloat(a.FixedLots)
	}
	if a.SpreadTicks > 0 {
		cfg.Spec.Costs.SpreadTicks = a.SpreadTicks
	}
	if a.CommissionBps > 0 {
		cfg.Spec.Costs.CommissionMode = instrument.CommissionBps
		cfg.Spec.Costs.CommissionBps = core.RatioFromFloat(a.CommissionBps / 10000)
	}
	if a.CommissionFlat > 0 {
		cfg.Spec.Costs.CommissionMode = instrument.CommissionPerTrade
		cfg.Spec.Costs.CommissionFlat = a.CommissionFlat
	}
	if a.SlippageTicks > 0 {
		cfg.Slippage = sim.SlippageModel{Enabled: true, Mode: instrument.SlippageFixed, Ticks: core.Price(a.SlippageTicks)}
	}
	return cfg, nil
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

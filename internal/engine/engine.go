package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/ir"
	_ "github.com/Bcypher01/Assay/internal/packs/indicator"
	_ "github.com/Bcypher01/Assay/internal/packs/priceaction"
	_ "github.com/Bcypher01/Assay/internal/packs/smc"
	_ "github.com/Bcypher01/Assay/internal/packs/timeofday"
	"github.com/Bcypher01/Assay/internal/primitive"
	"github.com/Bcypher01/Assay/internal/sim"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Data struct {
	Series, Fine *core.Series
	Config       sim.Config
	Snapshot     string
	Cache        primitive.Cache
	Registry     *primitive.Registry
}
type NearMiss struct {
	Bars         []Miss
	SlotFailures map[string]int
}
type Miss struct {
	Bar    int
	Held   []string
	Failed []string
}

// Run evaluates an IR document and passes its sparse entry plan to the
// simulator. Primitives remain self-contained: adding one only registers it.
func Run(ctx context.Context, strategy *ir.Strategy, data Data) (sim.RunResult, NearMiss, error) {
	var result sim.RunResult
	miss := NearMiss{SlotFailures: make(map[string]int)}
	if strategy == nil || data.Series == nil {
		return result, miss, fmt.Errorf("engine: strategy and series are required")
	}
	reg := data.Registry
	if reg == nil {
		reg = primitive.DefaultRegistry
	}
	if errs := strategy.Validate(reg); len(errs) > 0 {
		return result, miss, errs[0]
	}
	values := make(map[string]primitive.Value, len(strategy.Nodes))
	for _, n := range strategy.Nodes {
		p, e := reg.Get(n.Pack, n.Type)
		if e != nil {
			return result, miss, e
		}
		params, e := decodeParams(p, n.Params)
		if e != nil {
			return result, miss, fmt.Errorf("engine: node %s: %w", n.ID, e)
		}
		tf := n.Timeframe
		if !tf.Valid() {
			tf = data.Series.TF
		}
		v, _, e := primitive.Evaluate(ctx, p, params, tf, data.Series, data.Snapshot, data.Cache)
		if e != nil {
			return result, miss, e
		}
		if v.Len() != data.Series.Len() {
			return result, miss, fmt.Errorf("engine: node %s timeframe output cannot be aligned to entry series", n.ID)
		}
		values[n.ID] = v
	}
	sig := sim.Signals{Enter: core.NewBitset(data.Series.Len())}
	poiAt := -1
	for bar := 0; bar < data.Series.Len(); bar++ {
		bias := holds(strategy.Slots.Bias, values, bar)
		poi := holds(strategy.Slots.POI, values, bar)
		trigger := holds(strategy.Slots.Trigger, values, bar)
		if poi {
			poiAt = bar
		}
		triggerOK := trigger
		if strategy.Slots.Trigger.WithinBars > 0 {
			triggerOK = trigger && poiAt >= 0 && bar-poiAt <= strategy.Slots.Trigger.WithinBars
		}
		if bias && poi && triggerOK {
			addSides(&sig, strategy.Direction, bar, values[strategy.Slots.Invalidation].Level[bar], values[strategy.Slots.Target].Level[bar])
		} else {
			m := Miss{Bar: bar}
			if !bias {
				m.Failed = append(m.Failed, "bias")
				miss.SlotFailures["bias"]++
			} else {
				m.Held = append(m.Held, "bias")
			}
			if !poi {
				m.Failed = append(m.Failed, "poi")
				miss.SlotFailures["poi"]++
			} else {
				m.Held = append(m.Held, "poi")
			}
			if !triggerOK {
				m.Failed = append(m.Failed, "trigger")
				miss.SlotFailures["trigger"]++
			} else {
				m.Held = append(m.Held, "trigger")
			}
			miss.Bars = append(miss.Bars, m)
		}
	}
	if err := sim.Run(ctx, data.Series, data.Fine, sig, data.Config, &result); err != nil {
		return result, miss, err
	}
	return result, miss, nil
}
func decodeParams(p primitive.Primitive, raw json.RawMessage) (proto.Message, error) {
	m, err := parameterMessage(string(p.Spec().Params.FullName()))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return m, nil
	}
	if err := protojson.Unmarshal(raw, m); err != nil {
		return nil, err
	}
	return m, nil
}
func parameterMessage(name string) (proto.Message, error) {
	switch name {
	case "assay.v1.SampleParams":
		return &assayv1.SampleParams{}, nil
	case "assay.v1.MovingAverageParams":
		return &assayv1.MovingAverageParams{}, nil
	case "assay.v1.RsiParams":
		return &assayv1.RsiParams{}, nil
	case "assay.v1.AtrParams":
		return &assayv1.AtrParams{}, nil
	case "assay.v1.PeriodParams":
		return &assayv1.PeriodParams{}, nil
	case "assay.v1.SlopeParams":
		return &assayv1.SlopeParams{}, nil
	case "assay.v1.MovingAverageTouchParams":
		return &assayv1.MovingAverageTouchParams{}, nil
	case "assay.v1.MovingAverageCrossParams":
		return &assayv1.MovingAverageCrossParams{}, nil
	case "assay.v1.MacdParams":
		return &assayv1.MacdParams{}, nil
	case "assay.v1.BollingerBandsParams":
		return &assayv1.BollingerBandsParams{}, nil
	case "assay.v1.AdxParams":
		return &assayv1.AdxParams{}, nil
	case "assay.v1.StochasticParams":
		return &assayv1.StochasticParams{}, nil
	case "assay.v1.SwingParams":
		return &assayv1.SwingParams{}, nil
	case "assay.v1.ClusterParams":
		return &assayv1.ClusterParams{}, nil
	case "assay.v1.PinBarParams":
		return &assayv1.PinBarParams{}, nil
	case "assay.v1.DojiParams":
		return &assayv1.DojiParams{}, nil
	case "assay.v1.RangeParams":
		return &assayv1.RangeParams{}, nil
	case "assay.v1.MeasuredMoveParams":
		return &assayv1.MeasuredMoveParams{}, nil
	case "assay.v1.SessionParams":
		return &assayv1.SessionParams{}, nil
	case "assay.v1.DayOfWeekParams":
		return &assayv1.DayOfWeekParams{}, nil
	case "assay.v1.TimeOfDayRangeParams":
		return &assayv1.TimeOfDayRangeParams{}, nil
	case "assay.v1.BarOfSessionParams":
		return &assayv1.BarOfSessionParams{}, nil
	case "assay.v1.MarketStructureParams":
		return &assayv1.MarketStructureParams{}, nil
	case "assay.v1.DisplacementParams":
		return &assayv1.DisplacementParams{}, nil
	case "assay.v1.FairValueGapParams":
		return &assayv1.FairValueGapParams{}, nil
	case "assay.v1.OrderBlockParams":
		return &assayv1.OrderBlockParams{}, nil
	case "assay.v1.BreakerParams":
		return &assayv1.BreakerParams{}, nil
	case "assay.v1.LiquidityPoolParams":
		return &assayv1.LiquidityPoolParams{}, nil
	case "assay.v1.SweepParams":
		return &assayv1.SweepParams{}, nil
	case "assay.v1.PremiumDiscountParams":
		return &assayv1.PremiumDiscountParams{}, nil
	default:
		return nil, fmt.Errorf("engine: unsupported parameter schema %q", name)
	}
}
func holds(slot ir.SlotSpec, vals map[string]primitive.Value, bar int) bool {
	for _, id := range slot.All {
		v := vals[id]
		if v.Valid == nil || !v.Valid.Get(bar) || v.Bool == nil || !v.Bool.Get(bar) {
			return false
		}
	}
	return true
}
func addSides(sig *sim.Signals, d ir.Direction, bar int, stop, target core.Price) {
	sig.Enter.Set(bar)
	add := func(side core.Side) {
		sig.Side = append(sig.Side, side)
		sig.StopLevel = append(sig.StopLevel, stop)
		sig.TargetPlan = append(sig.TargetPlan, sim.TargetPlan{Level: target})
	}
	switch d {
	case ir.DirectionShort:
		add(core.SideShort)
	default:
		add(core.SideLong)
	}
}

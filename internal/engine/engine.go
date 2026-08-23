package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/ir"
	_ "github.com/Bcypher01/Assay/internal/packs/indicator"
	_ "github.com/Bcypher01/Assay/internal/packs/priceaction"
	_ "github.com/Bcypher01/Assay/internal/packs/timeofday"
	"github.com/Bcypher01/Assay/internal/primitive"
	"github.com/Bcypher01/Assay/internal/sim"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
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
	m := dynamicpb.NewMessage(p.Spec().Params)
	if len(raw) == 0 || string(raw) == "null" {
		return m, nil
	}
	if err := protojson.Unmarshal(raw, m); err != nil {
		return nil, err
	}
	return m, nil
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

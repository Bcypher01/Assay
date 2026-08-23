package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/primitive"
)

const SchemaV1 = "assay.strategy/v1"

type Direction string

const (
	DirectionLong  Direction = "long"
	DirectionShort Direction = "short"
	DirectionBoth  Direction = "both"
)

type Universe struct {
	Symbols []string `json:"symbols"`
}
type Risk struct {
	Stop     core.Distance `json:"stop"`
	Target   core.Distance `json:"target"`
	PerTrade core.Ratio    `json:"perTrade"`
}
type Node struct {
	ID        string          `json:"id"`
	Pack      string          `json:"pack"`
	Type      string          `json:"type"`
	Params    json.RawMessage `json:"params"`
	Timeframe core.TF         `json:"timeframe"`
}
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type SlotSpec struct {
	All        []string `json:"all"`
	WithinBars int      `json:"withinBars,omitempty"`
}
type Slots struct {
	Bias         SlotSpec `json:"bias"`
	POI          SlotSpec `json:"poi"`
	Trigger      SlotSpec `json:"trigger"`
	Invalidation string   `json:"invalidation"`
	Target       string   `json:"target"`
}
type Strategy struct {
	Schema     string             `json:"schema"`
	Name       string             `json:"name"`
	Universe   Universe           `json:"universe"`
	Timeframes map[string]core.TF `json:"timeframes"`
	Risk       Risk               `json:"risk"`
	Direction  Direction          `json:"direction"`
	Nodes      []Node             `json:"nodes"`
	Slots      Slots              `json:"slots"`
	Edges      []Edge             `json:"edges,omitempty"`
}
type ValidationError struct{ Path, Message string }

func (e ValidationError) Error() string { return e.Path + ": " + e.Message }

// Validate reports all document errors. Primitive lookup is deliberately here:
// authoring clients receive a field error before they ever start a run.
func (s *Strategy) Validate(reg *primitive.Registry) []ValidationError {
	var out []ValidationError
	add := func(p, m string) { out = append(out, ValidationError{p, m}) }
	if s.Schema != "" && s.Schema != SchemaV1 {
		add("schema", "unsupported schema")
	}
	if s.Name == "" {
		add("name", "required")
	}
	if len(s.Universe.Symbols) == 0 {
		add("universe.symbols", "required")
	}
	if s.Direction != "" && s.Direction != DirectionLong && s.Direction != DirectionShort && s.Direction != DirectionBoth {
		add("direction", "must be long, short, or both")
	}
	for _, name := range []string{"bias", "poi", "entry"} {
		if _, ok := s.Timeframes[name]; !ok {
			add("timeframes."+name, "required")
		}
	}
	if b, ok := s.Timeframes["bias"]; ok {
		if p, ok := s.Timeframes["poi"]; ok && !b.Coarser(p) && b != p {
			add("timeframes.poi", "must not be coarser than bias")
		}
		if e, ok := s.Timeframes["entry"]; ok && !b.Coarser(e) && b != e {
			add("timeframes.entry", "must not be coarser than bias")
		}
	}
	if s.Risk.Stop.Value <= 0 {
		add("risk.stop", "must be positive")
	}
	if s.Risk.Target.Value <= 0 {
		add("risk.target", "must be positive")
	}
	ids := map[string]Node{}
	for i, n := range s.Nodes {
		p := fmt.Sprintf("nodes[%d]", i)
		if n.ID == "" {
			add(p+".id", "required")
			continue
		}
		if _, ok := ids[n.ID]; ok {
			add(p+".id", "duplicate")
		}
		ids[n.ID] = n
		if reg != nil {
			if _, err := reg.Get(n.Pack, n.Type); err != nil {
				add(p, "unknown primitive")
			}
		}
	}
	check := func(name string, slot SlotSpec) {
		if len(slot.All) > 3 {
			add("slots."+name+".all", "maximum is 3")
		}
		for i, id := range slot.All {
			n, ok := ids[id]
			if !ok {
				add(fmt.Sprintf("slots.%s.all[%d]", name, i), "unknown node")
				continue
			}
			if reg != nil {
				p, _ := reg.Get(n.Pack, n.Type)
				if p != nil && p.Spec().Out != primitive.KindBool {
					add(fmt.Sprintf("slots.%s.all[%d]", name, i), "must reference a boolean primitive")
				}
			}
		}
		if slot.WithinBars < 0 {
			add("slots."+name+".withinBars", "must not be negative")
		}
	}
	check("bias", s.Slots.Bias)
	check("poi", s.Slots.POI)
	check("trigger", s.Slots.Trigger)
	for name, id := range map[string]string{"invalidation": s.Slots.Invalidation, "target": s.Slots.Target} {
		n, ok := ids[id]
		if !ok {
			add("slots."+name, "unknown node")
			continue
		}
		if reg != nil {
			p, _ := reg.Get(n.Pack, n.Type)
			if p != nil && p.Spec().Out != primitive.KindLevel {
				add("slots."+name, "must reference a level primitive")
			}
		}
	}
	return out
}

// Hash is a SHA-256 of canonical JSON: object keys are sorted, and omitted
// schema/direction defaults are normalised before marshaling.
func (s *Strategy) Hash() string {
	c := *s
	if c.Schema == "" {
		c.Schema = SchemaV1
	}
	if c.Direction == "" {
		c.Direction = DirectionBoth
	}
	b, _ := canonicalJSON(c)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func canonicalJSON(v any) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	var x any
	if e = json.Unmarshal(b, &x); e != nil {
		return nil, e
	}
	return canonical(x), nil
}
func canonical(v any) []byte {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b := []byte{'{'}
		for i, k := range keys {
			if i > 0 {
				b = append(b, ',')
			}
			q, _ := json.Marshal(k)
			b = append(b, q...)
			b = append(b, ':')
			b = append(b, canonical(x[k])...)
		}
		return append(b, '}')
	case []any:
		b := []byte{'['}
		for i, z := range x {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, canonical(z)...)
		}
		return append(b, ']')
	default:
		b, _ := json.Marshal(x)
		return b
	}
}

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

const (
	SchemaV1 = "assay.strategy/v1"
	SchemaV2 = "assay.strategy/v2"
)

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

// EntryTiming defines when an entry signal becomes an order. The simulator only
// supports next-bar open execution today; keeping this explicit prevents an
// authoring client from implying unsupported fill behaviour.
type EntryTiming string

const EntryTimingNextOpen EntryTiming = "next_open"

type Template struct {
	ID      string `json:"id"`
	Version uint32 `json:"version"`
}

// Expression is a typed boolean expression tree. Exactly one form is allowed:
// a primitive node reference, a non-empty all/any group, or a not group with one
// child. Leaves are intentionally restricted to boolean primitives; comparisons
// will be added only with explicit input and unit semantics.
type Expression struct {
	Node string       `json:"node,omitempty"`
	All  []Expression `json:"all,omitempty"`
	Any  []Expression `json:"any,omitempty"`
	Not  *Expression  `json:"not,omitempty"`
}

type EntryBranch struct {
	Enabled bool       `json:"enabled"`
	When    Expression `json:"when"`
}

type Entries struct {
	Timing EntryTiming `json:"timing,omitempty"`
	Long   EntryBranch `json:"long"`
	Short  EntryBranch `json:"short"`
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
	// Template and Entries are required by schema v2. Slots and Direction remain
	// readable for v1 documents and are deterministically migrated to Entries.
	Template Template `json:"template,omitempty"`
	Entries  Entries  `json:"entries,omitempty"`
}
type ValidationError struct{ Path, Message string }

func (e ValidationError) Error() string { return e.Path + ": " + e.Message }

// Validate reports all document errors. Primitive lookup is deliberately here:
// authoring clients receive a field error before they ever start a run.
func (s *Strategy) Validate(reg *primitive.Registry) []ValidationError {
	var out []ValidationError
	add := func(p, m string) { out = append(out, ValidationError{p, m}) }
	if s.Schema != "" && s.Schema != SchemaV1 && s.Schema != SchemaV2 {
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
	if s.Schema == SchemaV2 && s.Direction != "" {
		add("direction", "must be omitted for schema v2; use entries.long and entries.short")
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
	if s.Schema != SchemaV2 {
		check("bias", s.Slots.Bias)
		check("poi", s.Slots.POI)
		check("trigger", s.Slots.Trigger)
		if len(s.Slots.Bias.All) == 0 && len(s.Slots.POI.All) == 0 && len(s.Slots.Trigger.All) == 0 {
			add("slots.trigger.all", "add at least one boolean entry condition")
		}
	} else {
		validateV2Entries(add, s.Entries, ids, reg)
		if !s.Slots.empty() {
			add("slots", "must be empty for schema v2; use entries instead")
		}
		if s.Template.ID == "" {
			add("template.id", "required")
		}
		if s.Template.Version == 0 {
			add("template.version", "must be positive")
		}
	}
	if (s.Slots.Invalidation == "") != (s.Slots.Target == "") {
		add("slots.invalidation", "set both price levels or leave both blank to use the stop and target distances")
		add("slots.target", "set both price levels or leave both blank to use the stop and target distances")
	}
	for _, slot := range []struct {
		name string
		id   string
	}{
		{name: "invalidation", id: s.Slots.Invalidation},
		{name: "target", id: s.Slots.Target},
	} {
		name, id := slot.name, slot.id
		if id == "" {
			continue
		}
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
	if c.Schema == SchemaV2 {
		c.Direction = ""
		if c.Entries.Timing == "" {
			c.Entries.Timing = EntryTimingNextOpen
		}
	}
	var b []byte
	if c.Schema == SchemaV2 {
		b, _ = canonicalJSON(c)
	} else {
		// Keep the v1 canonical representation byte-for-byte compatible with
		// documents written before v2 fields existed. Run identity and trial
		// counting must not change merely because the application was upgraded.
		v1 := struct {
			Schema     string             `json:"schema"`
			Name       string             `json:"name"`
			Universe   Universe           `json:"universe"`
			Timeframes map[string]core.TF `json:"timeframes"`
			Risk       Risk               `json:"risk"`
			Direction  Direction          `json:"direction"`
			Nodes      []Node             `json:"nodes"`
			Slots      Slots              `json:"slots"`
			Edges      []Edge             `json:"edges,omitempty"`
		}{
			Schema: c.Schema, Name: c.Name, Universe: c.Universe, Timeframes: c.Timeframes,
			Risk: c.Risk, Direction: c.Direction, Nodes: c.Nodes, Slots: c.Slots, Edges: c.Edges,
		}
		b, _ = canonicalJSON(v1)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func validateV2Entries(add func(string, string), entries Entries, ids map[string]Node, reg *primitive.Registry) {
	if entries.Timing != "" && entries.Timing != EntryTimingNextOpen {
		add("entries.timing", "only next_open is currently supported")
	}
	branches := []struct {
		name   string
		branch EntryBranch
	}{
		{name: "long", branch: entries.Long},
		{name: "short", branch: entries.Short},
	}
	enabled := 0
	for _, x := range branches {
		path := "entries." + x.name
		if x.branch.Enabled {
			enabled++
			validateExpression(add, path+".when", x.branch.When, ids, reg)
		} else if !x.branch.When.empty() {
			add(path+".when", "must be empty when the branch is disabled")
		}
	}
	if enabled == 0 {
		add("entries", "enable at least one entry branch")
	}
}

func validateExpression(add func(string, string), path string, e Expression, ids map[string]Node, reg *primitive.Registry) {
	forms := 0
	if e.Node != "" {
		forms++
	}
	if len(e.All) != 0 {
		forms++
	}
	if len(e.Any) != 0 {
		forms++
	}
	if e.Not != nil {
		forms++
	}
	if forms != 1 {
		add(path, "must contain exactly one of node, all, any, or not")
		return
	}
	if e.Node != "" {
		n, ok := ids[e.Node]
		if !ok {
			add(path+".node", "unknown node")
			return
		}
		if reg != nil {
			p, _ := reg.Get(n.Pack, n.Type)
			if p != nil && p.Spec().Out != primitive.KindBool {
				add(path+".node", "must reference a boolean primitive")
			}
		}
		return
	}
	if e.Not != nil {
		validateExpression(add, path+".not", *e.Not, ids, reg)
		return
	}
	children, kind := e.All, "all"
	if len(e.Any) != 0 {
		children, kind = e.Any, "any"
	}
	for i, child := range children {
		validateExpression(add, fmt.Sprintf("%s.%s[%d]", path, kind, i), child, ids, reg)
	}
}

func (e Expression) empty() bool {
	return e.Node == "" && len(e.All) == 0 && len(e.Any) == 0 && e.Not == nil
}

func (s Slots) empty() bool {
	return len(s.Bias.All) == 0 && s.Bias.WithinBars == 0 &&
		len(s.POI.All) == 0 && s.POI.WithinBars == 0 &&
		len(s.Trigger.All) == 0 && s.Trigger.WithinBars == 0 &&
		s.Invalidation == "" && s.Target == ""
}

// MigrateV1ToV2 returns a deterministic v2 document with the same v1 entry
// condition and direction semantics. The legacy template identity distinguishes
// migrated documents from authored templates without inventing a user-facing
// strategy name.
func (s *Strategy) MigrateV1ToV2() (*Strategy, error) {
	if s == nil {
		return nil, fmt.Errorf("strategy: cannot migrate nil document")
	}
	if s.Schema != "" && s.Schema != SchemaV1 {
		return nil, fmt.Errorf("strategy: can only migrate schema v1")
	}
	if s.Slots.Trigger.WithinBars != 0 {
		return nil, fmt.Errorf("strategy: v1 trigger withinBars is not representable in schema v2")
	}
	c := *s
	c.Schema = SchemaV2
	c.Direction = ""
	c.Template = Template{ID: "legacy-v1", Version: 1}
	c.Entries = Entries{Timing: EntryTimingNextOpen}
	when := legacyExpression(s.Slots)
	direction := s.Direction
	if direction == "" {
		direction = DirectionBoth
	}
	if direction == DirectionLong || direction == DirectionBoth {
		c.Entries.Long = EntryBranch{Enabled: true, When: when}
	}
	if direction == DirectionShort || direction == DirectionBoth {
		c.Entries.Short = EntryBranch{Enabled: true, When: when}
	}
	c.Slots = Slots{}
	return &c, nil
}

func legacyExpression(slots Slots) Expression {
	children := make([]Expression, 0, len(slots.Bias.All)+len(slots.POI.All)+len(slots.Trigger.All))
	for _, slot := range []SlotSpec{slots.Bias, slots.POI, slots.Trigger} {
		for _, id := range slot.All {
			children = append(children, Expression{Node: id})
		}
	}
	if len(children) == 1 {
		return children[0]
	}
	return Expression{All: children}
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

package ir

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
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
// a primitive node reference, a typed predicate, a non-empty all/any group, or
// a not group with one child. Predicate inputs and output kinds are validated
// before execution.
type Expression struct {
	Node       string               `json:"node,omitempty"`
	Comparison *NumberComparison    `json:"comparison,omitempty"`
	Price      *PriceLevelPredicate `json:"price,omitempty"`
	Zone       *ZonePredicate       `json:"zone,omitempty"`
	Within     *OccurredWithin      `json:"within,omitempty"`
	Persists   *RemainsTrue         `json:"persists,omitempty"`
	After      *OccursAfter         `json:"after,omitempty"`
	All        []Expression         `json:"all,omitempty"`
	Any        []Expression         `json:"any,omitempty"`
	Not        *Expression          `json:"not,omitempty"`
}

type PriceSource string

const (
	PriceOpen  PriceSource = "open"
	PriceHigh  PriceSource = "high"
	PriceLow   PriceSource = "low"
	PriceClose PriceSource = "close"
)

type PriceRelation string

const (
	PriceAbove        PriceRelation = "above"
	PriceBelow        PriceRelation = "below"
	PriceCrossesAbove PriceRelation = "crosses_above"
	PriceCrossesBelow PriceRelation = "crosses_below"
)

// PriceLevelPredicate compares an explicit OHLC source against a level
// primitive. Cross predicates compare only this and the preceding closed bar.
type PriceLevelPredicate struct {
	Node     string        `json:"node"`
	Source   PriceSource   `json:"source"`
	Relation PriceRelation `json:"relation"`
}

type ZoneRelation string

const (
	ZoneInside  ZoneRelation = "inside"
	ZoneTouches ZoneRelation = "touches"
	ZoneLeaves  ZoneRelation = "leaves"
)

// ZonePredicate compares the current bar with a zone primitive. Inside uses
// the close; touches uses the inclusive high-low range; leaves requires the
// preceding close to be inside its zone and the current close to be outside.
type ZonePredicate struct {
	Node     string       `json:"node"`
	Relation ZoneRelation `json:"relation"`
}

// OccurredWithin is true when Condition held on this bar or one of the prior
// Bars bars. It is causal: future bars are never consulted.
type OccurredWithin struct {
	Condition Expression `json:"condition"`
	Bars      int        `json:"bars"`
}

// RemainsTrue is true only when Condition held on this bar and the preceding
// Bars-1 bars.
type RemainsTrue struct {
	Condition Expression `json:"condition"`
	Bars      int        `json:"bars"`
}

// OccursAfter is true when Event holds now and Predecessor held on an earlier
// bar no more than WithinBars bars ago.
type OccursAfter struct {
	Event       Expression `json:"event"`
	Predecessor Expression `json:"predecessor"`
	WithinBars  int        `json:"withinBars"`
}

type ComparisonOp string

const (
	ComparisonGreaterThan  ComparisonOp = "gt"
	ComparisonLessThan     ComparisonOp = "lt"
	ComparisonCrossesAbove ComparisonOp = "crosses_above"
	ComparisonCrossesBelow ComparisonOp = "crosses_below"
)

// NumberComparison evaluates a numeric primitive against an explicit
// fixed-point threshold. It is intentionally limited to numeric outputs;
// price and zone predicates need instrument-aware semantics of their own.
type NumberComparison struct {
	Node      string       `json:"node"`
	Operator  ComparisonOp `json:"operator"`
	Threshold core.Ratio   `json:"threshold"`
}

func (c NumberComparison) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Node      string       `json:"node"`
		Operator  ComparisonOp `json:"operator"`
		Threshold float64      `json:"threshold"`
	}{Node: c.Node, Operator: c.Operator, Threshold: c.Threshold.Float64()})
}

func (c *NumberComparison) UnmarshalJSON(data []byte) error {
	var raw struct {
		Node      string       `json:"node"`
		Operator  ComparisonOp `json:"operator"`
		Threshold json.Number  `json:"threshold"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("number comparison: decode: %w", err)
	}
	threshold, err := raw.Threshold.Float64()
	if err != nil {
		return fmt.Errorf("number comparison: threshold: %w", err)
	}
	*c = NumberComparison{Node: raw.Node, Operator: raw.Operator, Threshold: core.RatioFromFloat(threshold)}
	return nil
}

type EntryBranch struct {
	Enabled bool       `json:"enabled"`
	Name    string     `json:"name,omitempty"`
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
			if x.branch.Name == "" {
				add(path+".name", "required")
			}
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
	if e.Comparison != nil {
		forms++
	}
	if e.Price != nil {
		forms++
	}
	if e.Zone != nil {
		forms++
	}
	if e.Within != nil {
		forms++
	}
	if e.Persists != nil {
		forms++
	}
	if e.After != nil {
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
		add(path, "must contain exactly one of node, comparison, price, zone, within, persists, after, all, any, or not")
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
	if e.Comparison != nil {
		comparison := e.Comparison
		if comparison.Node == "" {
			add(path+".comparison.node", "required")
		} else if n, ok := ids[comparison.Node]; !ok {
			add(path+".comparison.node", "unknown node")
		} else if reg != nil {
			p, _ := reg.Get(n.Pack, n.Type)
			if p != nil && p.Spec().Out != primitive.KindNumber {
				add(path+".comparison.node", "must reference a numeric primitive")
			}
		}
		if comparison.Operator != ComparisonGreaterThan && comparison.Operator != ComparisonLessThan && comparison.Operator != ComparisonCrossesAbove && comparison.Operator != ComparisonCrossesBelow {
			add(path+".comparison.operator", "must be gt, lt, crosses_above, or crosses_below")
		}
		return
	}
	if e.Price != nil {
		validatePricePredicate(add, path+".price", *e.Price, ids, reg)
		return
	}
	if e.Zone != nil {
		validateZonePredicate(add, path+".zone", *e.Zone, ids, reg)
		return
	}
	if e.Within != nil {
		validateTemporal(add, path+".within", e.Within.Condition, e.Within.Bars, ids, reg)
		return
	}
	if e.Persists != nil {
		validateTemporal(add, path+".persists", e.Persists.Condition, e.Persists.Bars, ids, reg)
		return
	}
	if e.After != nil {
		if e.After.WithinBars <= 0 {
			add(path+".after.withinBars", "must be positive")
		}
		validateExpression(add, path+".after.event", e.After.Event, ids, reg)
		validateExpression(add, path+".after.predecessor", e.After.Predecessor, ids, reg)
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

func validatePricePredicate(add func(string, string), path string, predicate PriceLevelPredicate, ids map[string]Node, reg *primitive.Registry) {
	if predicate.Node == "" {
		add(path+".node", "required")
	} else if n, ok := ids[predicate.Node]; !ok {
		add(path+".node", "unknown node")
	} else if reg != nil {
		p, _ := reg.Get(n.Pack, n.Type)
		if p != nil && p.Spec().Out != primitive.KindLevel {
			add(path+".node", "must reference a level primitive")
		}
	}
	if predicate.Source != PriceOpen && predicate.Source != PriceHigh && predicate.Source != PriceLow && predicate.Source != PriceClose {
		add(path+".source", "must be open, high, low, or close")
	}
	if predicate.Relation != PriceAbove && predicate.Relation != PriceBelow && predicate.Relation != PriceCrossesAbove && predicate.Relation != PriceCrossesBelow {
		add(path+".relation", "must be above, below, crosses_above, or crosses_below")
	}
}

func validateZonePredicate(add func(string, string), path string, predicate ZonePredicate, ids map[string]Node, reg *primitive.Registry) {
	if predicate.Node == "" {
		add(path+".node", "required")
	} else if n, ok := ids[predicate.Node]; !ok {
		add(path+".node", "unknown node")
	} else if reg != nil {
		p, _ := reg.Get(n.Pack, n.Type)
		if p != nil && p.Spec().Out != primitive.KindZone {
			add(path+".node", "must reference a zone primitive")
		}
	}
	if predicate.Relation != ZoneInside && predicate.Relation != ZoneTouches && predicate.Relation != ZoneLeaves {
		add(path+".relation", "must be inside, touches, or leaves")
	}
}

func validateTemporal(add func(string, string), path string, condition Expression, bars int, ids map[string]Node, reg *primitive.Registry) {
	if bars <= 0 {
		add(path+".bars", "must be positive")
	}
	validateExpression(add, path+".condition", condition, ids, reg)
}

func (e Expression) empty() bool {
	return e.Node == "" && e.Comparison == nil && e.Price == nil && e.Zone == nil && e.Within == nil && e.Persists == nil && e.After == nil && len(e.All) == 0 && len(e.Any) == 0 && e.Not == nil
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
		c.Entries.Long = EntryBranch{Enabled: true, Name: "Legacy long entry", When: when}
	}
	if direction == DirectionShort || direction == DirectionBoth {
		c.Entries.Short = EntryBranch{Enabled: true, Name: "Legacy short entry", When: when}
	}
	c.Slots = Slots{}
	return &c, nil
}

func legacyExpression(slots Slots) Expression {
	children := make([]Expression, 0, 3)
	if expression := slotExpression(slots.Bias); !expression.empty() {
		children = append(children, expression)
	}
	poi := slotExpression(slots.POI)
	if !poi.empty() {
		if slots.Trigger.WithinBars > 0 {
			poi = Expression{Within: &OccurredWithin{Condition: poi, Bars: slots.Trigger.WithinBars}}
		}
		children = append(children, poi)
	}
	if expression := slotExpression(slots.Trigger); !expression.empty() {
		children = append(children, expression)
	}
	if len(children) == 1 {
		return children[0]
	}
	return Expression{All: children}
}

func slotExpression(slot SlotSpec) Expression {
	if len(slot.All) == 1 {
		return Expression{Node: slot.All[0]}
	}
	if len(slot.All) > 1 {
		children := make([]Expression, 0, len(slot.All))
		for _, id := range slot.All {
			children = append(children, Expression{Node: id})
		}
		return Expression{All: children}
	}
	return Expression{}
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

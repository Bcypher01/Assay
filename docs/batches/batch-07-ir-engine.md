# Batch 07 — Strategy IR and engine

**Depends on:** 04, 05, 06
**Blocks:** 08, 10
**Packages:** `internal/ir`, `internal/engine`

## Goal

Define the document every authoring surface produces and the executor that turns
it into a run. This is the contract that lets the checklist ship now and a node
canvas ship later without touching the engine.

## Deliverables

`internal/ir`

- `Strategy` document types: universe, timeframes, risk, direction, nodes, slots,
  edges
- JSON serialisation, schema version, forward-compatible unknown-field handling
- Content hash over the canonical form, used for run identity and trial counting
- Validator producing field-path errors: slot output kinds match slot
  requirements, timeframes are ordered correctly, units are present on every
  distance, referenced node IDs exist, AND-stack limits respected
- `$side` binding resolution for long and short evaluation

`internal/engine`

- Slot executor: resolve nodes to primitives, evaluate via the memo cache, reduce
  slot conditions to signal arrays, hand to the simulator
- Trigger windowing (`withinBars` after a POI condition holds)
- Stop and target plan construction from the invalidation and target slots
- Near-miss capture: for each bar, which slot conditions held and which failed
- `Run(ctx, ir, data) (RunResult, error)`

## Key decisions

**The IR is a DAG with a slot map, not a five-field struct.** The checklist
writes the slot map and leaves `edges` empty; a canvas would write edges. If this
were modelled as five fields, the canvas becomes a rewrite and will never ship.

**Slots hold up to three AND-stacked conditions.** The pressure valve that delays
the canvas. More than three and the UI stops being a checklist.

**Validation is exhaustive and returns every error, not the first.** The UI
renders errors against the controls that caused them; failing fast would make
that impossible.

**Near-miss data is collected during the normal pass.** The engine already
evaluates every condition on every bar; recording which failed costs a counter
increment and answers the most common user question — "why so few trades?"

**Canonical form before hashing.** Field ordering, default elision and numeric
formatting are normalised, so two documents that mean the same thing hash the
same. Trial counting depends on it.

## Interfaces

```go
type Strategy struct {
    Schema     string
    Name       string
    Universe   Universe
    Timeframes map[string]core.TF   // bias, poi, entry
    Risk       Risk
    Direction  Direction
    Nodes      []Node
    Slots      Slots
    Edges      []Edge               // empty while slot-wired
}

type Slots struct {
    Bias, POI, Trigger SlotSpec     // All []string, plus WithinBars on Trigger
    Invalidation       string
    Target             string
}

func (s *Strategy) Validate() []ValidationError   // all errors, field paths
func (s *Strategy) Hash() string                  // canonical

func Run(ctx context.Context, s *Strategy, d Data) (sim.RunResult, NearMiss, error)
```

## Tests

- Validation: a table of malformed documents, each asserting the exact field path
  and message
- Kind matching: a Number primitive in the POI slot fails validation
- Timeframe ordering: an entry timeframe coarser than the bias timeframe fails
- Hashing: reordered fields, elided defaults and equivalent numeric formats hash
  identically; a changed parameter does not
- End to end: an example trend-pullback strategy runs against fixture data and
  produces the expected trade count
- `$side`: a both-directions strategy produces long and short trades from one
  document
- Near-miss counts reconcile against a hand-computed fixture

## Exit criteria

- [ ] Three example strategies across different styles run end to end on fixture
      data with expected results
- [ ] Every validation rule has a test asserting its field path
- [ ] Same document runs twice produce byte-identical results
- [ ] Adding a primitive requires no change to `internal/engine`

## Out of scope

The transport (08) and the UI. Sweeps and walk-forward (10). The canvas — `edges`
is defined and validated but not executed.

## Decision log

_Record the canonical-form rules and the schema-version compatibility policy._

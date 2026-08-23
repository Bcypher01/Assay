# Batch 05 — Primitive registry and memoisation

**Depends on:** 00, 01
**Blocks:** 06, 07, 09
**Packages:** `internal/primitive`, `proto/assay/v1`

## Goal

Define the one interface every detector implements, the registry that exposes
them to the UI, and the memoisation cache that makes sweeps tractable. The cache
is built here rather than added later — retrofitting a cache key onto primitives
that already mutate shared state is unpleasant.

## Deliverables

- `Primitive` and `Instance` interfaces
- Five value kinds: `Bool`, `Level`, `Zone`, `Number`, `State`
- `Overlay` types the chart renders: marker, level line, zone box, label
- Registry keyed by `(pack, type)`, with parameter schemas
- **Parameter definitions as proto messages** — one source generating the Go
  struct, the TypeScript type and the UI form schema
- Config hashing: stable hash over `(pack, type, params, timeframe, snapshot)`
- Memo cache: bounded, keyed by config hash, storing signal outputs and overlays
- Evaluation driver: run a set of primitives over a series once, honouring
  timeframe close semantics and the no-look-ahead rule

## Key decisions

**Parameters live in protos.** Otherwise the same schema is hand-maintained in
Go, TypeScript and the form generator, and drifts. This is the single largest
calendar-time saving available in the project.

**Memoise on config hash, not on object identity.** Two independently constructed
primitives with identical parameters must hit the same cache entry — that is the
entire point during a sweep.

**Primitives are pure with respect to a run.** All state lives in the `Instance`,
created per evaluation. A primitive that caches across runs breaks determinism
and the memo cache both.

**Interface dispatch happens once per detection pass, not per simulated bar.**
The two-phase execution model exists partly to keep the interface boundary out of
the innermost loop.

**The cache is bounded and reports hit rate.** An unbounded cache over a large
sweep exhausts memory; a silent one makes performance regressions invisible.

## Interfaces

```go
type Kind uint8
const (KindBool Kind = iota; KindLevel; KindZone; KindNumber; KindState)

type Spec struct {
    Pack, Type string
    Out        Kind
    Params     protoreflect.MessageDescriptor
    Timeframed bool
}

type Primitive interface {
    Spec() Spec
    New(params proto.Message, tf core.TF) (Instance, error)
}

type Instance interface {
    Eval(ctx *EvalCtx, s *core.Series) (Value, error)   // whole-series pass
    Overlays(from, to core.Time) []Overlay
}

type Cache interface {
    GetOrEval(key ConfigHash, f func() (Value, error)) (Value, error)
    Stats() (hits, misses int)
}
```

## Tests

- Registry: registering a duplicate `(pack, type)` fails loudly
- Hashing: parameter reordering, defaulted fields and numeric formatting produce
  the same hash; a changed value does not
- Cache: identical configs hit; differing snapshots miss; bound is respected
- Look-ahead: an H4 primitive evaluated over an M5 series sees a bar only after
  its H4 close — asserted with a probe primitive that records what it observed
- Determinism across concurrent evaluation of the same config

## Exit criteria

- [ ] A sweep of 48 configurations differing only in exit parameters performs
      exactly the number of detection passes the distinct entry configs require
- [ ] Cache hit rate is reported and asserted in a test
- [ ] Parameter schema for a sample primitive generates identical Go and
      TypeScript definitions from one proto
- [ ] The look-ahead probe test passes

## Out of scope

Actual detectors (06, 09). The IR (07). Chart rendering — this batch emits
overlay data, it does not draw.

## Decision log

_Record the proto codegen toolchain, and the cache bound and eviction policy._

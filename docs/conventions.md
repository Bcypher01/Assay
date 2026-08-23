# Go Conventions

Rules specific to this codebase. General Go style (Effective Go, the review
comments wiki) applies on top and is not restated here.

---

## Types

**Prices** are `core.Price`, an `int64` in instrument ticks. Construct only
through `instrument.Spec.PriceFromDecimal` / `.ToDecimal`. Arithmetic on prices
returns prices; ratios return `core.Ratio` (also fixed-point). A bare `float64`
holding a price is a bug.

**Timestamps** are `core.Time`, an `int64` of milliseconds since epoch, UTC only.
No `time.Time` in series, storage, or hot paths — convert at the display boundary.

**Timeframes** are a closed enum (`core.TF`), ordered, with `Duration()` and
`Coarser/Finer` helpers. Never a string outside serialisation.

**Distances** carry units: `core.Distance{Unit, Value}` where unit is one of
`ATR`, `Pips`, `Ticks`, `Percent`, `Price`. A bare number for a stop buffer or a
tolerance is a bug — it breaks the moment a strategy runs on a second instrument.

---

## Series

```go
type Series struct {
    TF     core.TF
    Time   []core.Time
    Open   []core.Price
    High   []core.Price
    Low    []core.Price
    Close  []core.Price
    Volume []int64
}
```

Struct-of-arrays, never `[]Bar`. All slices share a length; `Len()` asserts it in
tests, not in the hot loop. Series are immutable once loaded and shared by
pointer across goroutines — never mutate one after publication.

Boolean streams are `core.Bitset`, not `[]bool`.

---

## Interfaces

Interfaces are declared by the consumer, not the producer. `internal/engine`
declares what it needs from a primitive; `internal/packs/*` returns concrete
types that satisfy it.

Keep interfaces out of the innermost loop where the call count is bars ×
primitives × configurations. The two-phase execution model exists partly so the
interface boundary is crossed once per detection pass rather than once per
simulated bar.

---

## Allocation

In anything that runs per-bar or per-configuration:

- Preallocate and reset; do not reallocate.
- Accept output slices as parameters (`AppendX(dst []T, ...) []T`) rather than
  returning fresh ones.
- Pool run contexts with `sync.Pool`.
- No `fmt.Sprintf` for keys, no string concatenation, no `interface{}` boxing.
- Benchmark with `-benchmem`. A hot-path change that adds allocations needs a
  reason in the commit message.

---

## Concurrency

- One bounded worker pool per study, sized `runtime.GOMAXPROCS(0)`.
- Immutable inputs shared by pointer; each worker owns its mutable state.
- Results collected into a preallocated indexed slice, never appended from
  multiple goroutines and never assembled from a map.
- `context.Context` is the first parameter of anything cancellable, and
  cancellation is honoured inside long loops, not just between them.

---

## Determinism checklist

Applies to every package under `internal/`:

- [ ] No `range` over a map in any path affecting output
- [ ] No `time.Now()`
- [ ] No package-level `rand`; seeds passed explicitly
- [ ] No dependence on goroutine completion order
- [ ] Floating point not compared for equality
- [ ] Map keys sorted before iteration where iteration is unavoidable

---

## Naming

- Packages are singular nouns (`instrument`, not `instruments`).
- Constructors are `New<Type>`; ones that can fail return `(T, error)`.
- Detector types are named for what they detect: `FairValueGap`, `OrderBlock`,
  `LiquiditySweep` — never `FVGDetector` or `SMCHelper`.
- Test fixtures are named `<subject>_<case>.golden.json` under `testdata/golden/`.

---

## Dependencies

Adding one requires a note in the batch doc's decision log. The bar for a
dependency in `internal/core`, `internal/sim`, or `internal/primitive` is very
high — those are the packages that must stay fast, allocation-free and
cgo-free.

Currently sanctioned:

| Purpose | Choice |
|---|---|
| Parquet | pure-Go reader/writer, no cgo |
| RPC | `connectrpc.com/connect` |
| Protobuf | `google.golang.org/protobuf` |
| Statistics | `gonum.org/v1/gonum` (distributions only) |
| Object storage | S3-compatible client for R2 |

Explicitly rejected: DuckDB (cgo), dataframe libraries (wrong abstraction,
allocation-heavy), ORMs, reflection-based config loaders in hot paths.

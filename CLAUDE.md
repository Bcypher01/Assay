# Assay — Working Agreement

A no-code, multi-asset strategy backtester whose defining feature is that its
numbers are statistically defensible. Go engine, ConnectRPC transport, local web
UI, prebuilt bar library on object storage.

Full product and architecture plan: `docs/architecture.md`.
Work is organised into batches: `docs/batches/`.

---

## The pipeline

Every study flows through the same stages, in this order, with no shortcuts:

```
IR document → validate → primitives (memoised) → signal arrays → simulator → trades → analytics
```

One layer, one responsibility:

- **Primitives** detect. They never place orders, never see positions, never know
  about risk.
- **The simulator** executes. It never detects anything; it consumes signal arrays
  and OHLC and produces trades.
- **The engine** wires the two together from an IR document. It contains no
  detection logic and no fill logic of its own.
- **Analytics** orchestrates repeated engine runs. It never reaches into the
  simulator or primitives.
- **RPC handlers** are thin. No business logic, no computation, no branching on
  results.

If you are about to put detection logic in the simulator, or fill logic in a
primitive, stop — the design is telling you the boundary is in the wrong place.

---

## Before you write a single line

Read these if you have not already read them this session:

- `docs/architecture.md` — layering, data model, engine semantics
- `docs/conventions.md` — Go rules specific to this codebase
- The batch doc for what you are building — `docs/batches/batch-NN-*.md`

If any of them contradict what you have been asked to build, raise it rather than
picking one.

---

## Non-negotiables

**Prices are fixed-point.**
`core.Price` is an `int64` scaled by the instrument's tick size. Never `float64`
in storage, comparison, or arithmetic that feeds a decision. Floats are permitted
only in derived statistics that are never compared for equality. Level-touch
comparisons must be exact integer comparisons.

**Determinism is a hard requirement.**
A run is a pure function of `(IR document, data snapshot, seed)`.

- Never range over a map in any path that affects output. Sort keys explicitly.
- No `time.Now()`, no `rand` without an explicitly passed seed, anywhere under
  `internal/` except `cmd/` wiring and the pipeline job.
- No goroutine-ordering dependence in result assembly.
- Every study pins the data snapshot by manifest content hash.

There is a determinism test per batch that runs the same input twice and compares
bytes. It is not optional.

**No look-ahead.**
A higher-timeframe bar becomes visible only after it closes. Primitives receive
`OnBar` for closed bars of their own timeframe only. There is no API by which a
primitive can read a partial bar, and adding one is a design error, not a feature.

**Ambiguity is reported, never resolved by assumption.**
When a bar's range contains both stop and target and the available data cannot
order them, the trade is evaluated both ways and the study reports a range.
Picking the favourable branch is the single most common way backtesters lie.

**Series are struct-of-arrays.**
Separate typed slices per field. Not `[]Bar`. Boolean signal streams are bitsets,
not `[]bool`.

**No CGO.**
The binary must cross-compile statically. That rules out DuckDB and any
cgo-linked Parquet reader. If you believe a dependency needs cgo, raise it before
adding it.

**The hot path does not allocate.**
Per-run state is preallocated and reset, not reallocated. Bar arrays are shared
by pointer and never copied. Sweeps use a bounded worker pool sized to
`GOMAXPROCS`, never a goroutine per configuration.

---

## Errors

- Wrap with `fmt.Errorf("...: %w", err)`; preserve the chain.
- Sentinel errors live beside the package that returns them (`ErrNotFound`,
  `ErrUnsupportedResolution`).
- Never panic in library code. `cmd/` may fatal on startup failure.
- Validation errors from the IR carry the offending node ID and field path — the
  UI renders them next to the control that caused them.

---

## Testing

Every batch ships with:

- Table tests for pure logic
- A determinism test (same input twice, byte-identical output)
- Golden-file tests where output is structured — fixtures in `testdata/golden/`
- A benchmark for anything in the hot path, with `-benchmem`

Detectors additionally assert against the annotated golden chart set. Fill logic
is fuzzed.

Nothing is "done" until `make test`, `make race`, and `make lint` are clean.

---

## Batch workflow

1. Read the batch doc.
2. Implement in the order the doc specifies.
3. Self-review against `docs/conventions.md` and the batch's exit criteria.
4. Write tests.
5. Only then move to the next batch.

Do not start a batch whose dependencies are unfinished. The dependency graph is
in `docs/batches/README.md`, and it is there because several batches are cheap to
build wrong and expensive to fix.

---

## Performance targets

Design against these; profile with pprof rather than guessing.

| Operation | Target |
|---|---|
| Single run, 2.6M M1 bars, 5 primitives | < 1s on one core |
| 1,000-config sweep, memoised | < 60s on 8 cores |
| Overlay repaint, 5 years M15 | < 1s end to end including transport |
| Cold start to first backtest | < 10s including data fetch |

---

## What this project is not

Not a live trading system. No order routing, no broker credentials, no custody.
Not a signal service. Not a place for machine-learning strategy generation.

A backtest describes what a rule did on one historical sample. It does not
describe what it will do. Every surface this codebase produces should make that
harder to forget, not easier.

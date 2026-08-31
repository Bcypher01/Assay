---
name: assay-primitive
description: Add or change a detector in Assay - an indicator, price-action pattern, SMC structure, time-of-day rule or semantic setup. Use when asked to add a primitive, detector, indicator or signal, or to change how one detects. Covers the Primitive/Instance contract, parameter protos, registration, look-ahead rules and the golden-chart tests a detector must ship with.
---

# Adding a primitive

A primitive **detects**. It never places orders, never sees positions, never
knows about risk. If you find yourself reaching for a stop, a lot size or an
open position, you are writing simulator code in the wrong package.

## Where it goes

| Pack | For |
|---|---|
| `internal/packs/indicator` | deterministic numeric indicators (MA, RSI, ATR) |
| `internal/packs/priceaction` | bar-shape and swing patterns |
| `internal/packs/smc` | market-structure objects (FVG, order block, sweep) |
| `internal/packs/timeofday` | session and clock-anchored levels |
| `internal/packs/semantic` | chart-drawing setups compiled from the UI |

A new pack is a new directory with a package comment and an `init()` that
registers its primitives. Do not create one unless the detector genuinely fits
none of the above.

## The contract

```go
type Primitive interface {                    // a stateless factory
	Spec() Spec
	New(params proto.Message, tf core.TF) (Instance, error)
}

type Instance interface {                     // one evaluation's state
	Eval(ctx *EvalCtx, s *core.Series) (Value, error)
	Overlays(from, to core.Time) []Overlay
}
```

- The `Primitive` value retains **no** state between evaluations. State lives on
  the `Instance` returned by `New`.
- `Eval` runs once per detection pass over the whole series — not per simulated
  bar. Keep interface calls out of the inner loop.
- `Value.Kind` picks exactly one populated member (`Bool`, `Level`, `Zone`,
  `Number`, `State`). Boolean streams are `*core.Bitset`, never `[]bool`.
- `Value.Validate(s.Len())` must pass: one value per source bar. Use the
  `Valid` bitset to mark warm-up bars rather than emitting zeros.
- `Overlays` emits portable drawing data only. No rendering, no colours.

## Steps

1. **Parameters are a proto message.** Add or reuse one in
   `proto/assay/v1/primitive.proto`, then `make proto`. The message descriptor
   goes in `Spec.Params` — that is what the UI renders the controls from, and
   what validation errors are addressed against.
2. **Name it for what it detects**: `FairValueGap`, `LiquiditySweep` — never
   `FVGDetector` or `SMCHelper`.
3. **Implement `Eval` in fixed point.** Prices are `core.Price` (int64 ticks).
   Level touches are exact integer comparisons. Any tolerance or buffer is a
   `core.Distance` with a unit (`ATR`, `Pips`, `Ticks`, `Percent`, `Price`) —
   a bare number breaks the moment the strategy runs on a second instrument.
4. **Respect the timeframe.** `Eval` sees closed bars of its own timeframe
   only. There is no API for reading a partial bar; wanting one means the
   design is wrong. A higher-timeframe value becomes visible only after that
   bar closes.
5. **Register it** in the pack's `init()` via `primitive.MustRegister`.
   Duplicate `(pack, type)` pairs fail loudly at startup — that is intended.
6. **Preallocate.** `make([]core.Price, s.Len())` once; no `fmt.Sprintf`, no
   string concatenation, no `interface{}` boxing inside the loop.

## Tests it must ship with

- A table test over hand-checked bar sequences, including the warm-up boundary
  and the empty/short-series cases.
- An assertion against the annotated golden chart set:
  `internal/packs/<pack>/testdata/golden/<subject>_<case>.golden.json`.
- A determinism test: evaluate twice, compare bytes.
- A `-benchmem` benchmark if the detector runs per bar.

Then run the `assay-verify` skill.

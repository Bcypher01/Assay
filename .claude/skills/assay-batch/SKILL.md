---
name: assay-batch
description: Start, continue or finish a batch of Assay work as specified in docs/batches. Use when asked to implement a batch, pick up the next piece of planned work, or when unsure whether a batch's dependencies are done. Enforces the read-first order, the layering the pipeline requires, and the exit criteria.
---

# Working a batch

Work in this repo is organised into batches under `docs/batches/`. The
dependency graph in `docs/batches/README.md` exists because several batches are
cheap to build wrong and expensive to fix.

## Before writing a line

Read, if you have not already this session:

1. `docs/architecture.md` — layering, data model, engine semantics
2. `docs/conventions.md` — the Go rules specific to this codebase
3. `docs/batches/batch-NN-*.md` — the batch you are building

If any of them contradict what you were asked to build, say so rather than
picking one silently.

Do not start a batch whose dependencies are unfinished. Check the graph.

## The pipeline the work must fit

```
IR document → validate → primitives (memoised) → signal arrays → simulator → trades → analytics
```

One layer, one responsibility:

- **Primitives** detect. No orders, no positions, no risk.
- **The simulator** executes. It consumes signal arrays and OHLC; it detects
  nothing.
- **The engine** wires the two from an IR document. No detection, no fill logic.
- **Analytics** orchestrates repeated engine runs. It never reaches into the
  simulator or primitives.
- **RPC handlers** translate messages. No business logic.

Detection logic in the simulator, or fill logic in a primitive, means the
boundary is in the wrong place. Fix the boundary.

## Order of work

1. Implement in the order the batch doc specifies.
2. Self-review against `docs/conventions.md` and the batch's exit criteria.
3. Write the tests the batch requires: table tests, a determinism test, golden
   files where output is structured, a `-benchmem` benchmark for hot paths.
4. Record any new dependency in the batch doc's decision log. The bar is very
   high for `internal/core`, `internal/sim` and `internal/primitive`, and
   anything needing cgo is rejected — the binary must cross-compile statically.
5. Run the `assay-verify` skill. Only then move to the next batch.

## Ambiguity is reported, never resolved by assumption

When a bar's range contains both stop and target and the data cannot order
them, evaluate both ways and report a range. Picking the favourable branch is
the single most common way backtesters lie, and this product exists to not do
that. The same instinct applies to every decision in a batch: if the honest
answer is a range or an error, do not collapse it to a number.

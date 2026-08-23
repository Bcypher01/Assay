# Architecture

Condensed from the product plan. This file is the authority for layering and
package boundaries; the plan is the authority for product intent.

---

## Layers

| Layer | Package | Responsibility | Interface out |
|---|---|---|---|
| Core types | `internal/core` | Fixed-point price, timestamps, timeframes, series, bitsets | Value types |
| Instruments | `internal/instrument` | Tick size, contract size, currencies, costs, sessions, quality tier | `InstrumentSpec` |
| Storage | `internal/store` | Parquet bar format, manifests, local cache layout | `Series`, `Manifest` |
| Library | `internal/library` | Fetch published bar sets, verify, fall back to venue | `Loader` |
| Venues | `internal/venue/*` | Per-venue download and decode | `Fetcher` |
| Primitives | `internal/primitive`, `internal/packs/*` | Detection, overlays, memoisation | `Primitive`, `Instance` |
| Simulator | `internal/sim` | Orders, fills, costs, sizing, ambiguity bounding | `RunResult` |
| Engine | `internal/engine` | Execute an IR document end to end | `Run(ir, data) RunResult` |
| IR | `internal/ir` | Strategy document types, validation, hashing | `Strategy` |
| Analytics | `internal/analytics` | Walk-forward, sweeps, Monte Carlo, sensitivity | `StudyResult` |
| Run store | `internal/runstore` | Run history, trial counting, comparison | `Store` |
| RPC | `internal/rpc` | ConnectRPC handlers | Generated services |

Two binaries:

- `cmd/assay` — the user-facing binary. Serves the ConnectRPC API and the static UI
  on localhost, runs studies locally.
- `cmd/pipeline` — the bar library builder. Runs on a schedule, publishes Parquet
  to object storage. Ships to nobody. Shares only the Parquet schema with `assay`.

The pipeline is deliberately outside the app's dependency graph. It is the only
component with an uptime obligation, and taking it offline must never break a
client that has already fetched data.

---

## Execution model

Two phases, and the split matters more than any micro-optimisation:

**Phase 1 — signal generation.** Each primitive is evaluated over history once per
distinct `(type, params, timeframe, data snapshot)` hash, producing compact
signal arrays. Results are memoised, so a sweep over parameters that only affect
exits reuses one detection pass across every configuration.

**Phase 2 — trade simulation.** Walks signals plus OHLC and produces trades. No
interface dispatch, no allocation, cheap enough to run thousands of times.

A sweep over 8 RR values × 6 FVG sizes is 48 simulations but 6 detections.

---

## Data model

Bars are small; only ticks are large. One FX pair, seven years, Parquet:

| Resolution | Bars | Size |
|---|---|---|
| H4 | ~11k | 0.2 MB |
| M15 | ~175k | 3.5 MB |
| M1 | ~2.6M | 50 MB |
| Ticks | ~300M | 2.4 GB |

Consequences:

- Required resolution derives from the strategy's finest timeframe. The engine
  requests exactly that.
- Base resolution must be at least as fine as the strategy's finest timeframe.
  An M5 entry on M15 bars is refused, not degraded.
- Bars come from the published library — identical for every user, so no user
  recomputes them. Partitioned by symbol, timeframe and month; every file carries
  a manifest with row count, bounds, content hash, source and build version.
- Tick and 1-second data are never bulk-downloaded. They are fetched only for the
  specific windows containing unresolved trades, and only when the outcome bounds
  straddle a decision boundary.

---

## Ambiguity bounding

When a bar contains both stop and target, evaluate the trade both ways and carry
the pair through the study. The result is a range — `expectancy 0.18R to 0.31R,
47 of 214 trades unresolved` — not a point estimate produced by a fill
assumption.

This inverts the data problem: if the pessimistic bound is still an edge, finer
data changes no decision and is not fetched.

---

## Timeframes and look-ahead

Timeframes form a strict hierarchy. A primitive registered at H4 receives
`OnBar` only when an H4 bar closes. The engine's clock advances on the finest
timeframe in the strategy and fans closed bars upward. There is no path by which
a primitive observes a partial bar.

---

## Contracts

The two public contracts are the **IR document schema** (`internal/ir`) and the
**service protos** (`proto/assay/v1`). Everything else is internal and may change
freely. Primitive parameter definitions live in the protos so the Go struct, the
TypeScript type and the UI form schema come from one source and cannot drift.

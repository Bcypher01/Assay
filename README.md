# Assay

A no-code, multi-asset strategy backtester whose defining feature is that its
numbers are statistically defensible: walk-forward by default, outcome ranges
instead of assumed fills, sensitivity made visible, and multiple testing counted
and disclosed.

Go engine, ConnectRPC transport, local web UI, prebuilt bar library on object
storage. Runs entirely on the user's machine.

## Layout

```
cmd/assay          user-facing binary: serves API + UI on localhost
cmd/pipeline     bar library builder, runs on a schedule, ships to nobody

internal/core        fixed-point prices, time, timeframes, series, bitsets
internal/instrument  instrument specs, calendars, cost models
internal/store       Parquet bar format, manifests, local cache
internal/event       immutable structured-event snapshots and query model
internal/eventsource reviewed event-source adapters (curated catalogue first)
internal/eventpipeline imports event sources into local snapshots
internal/library     library fetch + direct-from-venue fallback
internal/venue/*     per-venue download and decode
internal/primitive   primitive interface, registry, memoisation cache
internal/packs/*     indicator, price-action, time and SMC detectors
internal/sim         trade simulation, costs, ambiguity bounding
internal/ir          strategy document, validation, content hashing
internal/engine      IR execution: primitives -> signals -> trades
internal/analytics   walk-forward, sweeps, Monte Carlo, sensitivity
internal/runstore    run history, trial counting, comparison
internal/rpc         ConnectRPC handlers

proto/assay/v1     service and primitive-parameter definitions
gen/             generated Go and TypeScript clients
docs/            architecture, conventions, batch specs
testdata/golden  golden fixtures, including annotated charts
```

## Getting started

```
make build      # bin/assay and bin/pipeline
make test
make race
make lint
make bench
```

To import ten years of official BLS CPI and Employment Situation releases:

```
go run ./cmd/event-pipeline -provider bls -dir event-data -build-version v1
```

The checked-in curated catalogue remains available only for offline fixtures:
`-provider curated`.

## Where to start reading

1. `CLAUDE.md` — working agreement and non-negotiables
2. `docs/architecture.md` — layering and data model
3. `docs/conventions.md` — Go rules specific to this codebase
4. `docs/batches/README.md` — the work, in order

## A note that belongs in the product too

A backtest describes what a rule did on one historical sample. It does not
describe what it will do.

## Private beta

The local first-run plan and supported-market/order limits are documented in
[the private-beta guide](docs/private-beta.md). Product instrumentation is off
by default; `assay -metrics-dir beta-metrics` records aggregate metrics only on
the user's machine after explicit consent.

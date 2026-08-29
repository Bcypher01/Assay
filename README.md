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
internal/packs/semantic chart-drawing setup detectors
internal/setuptest    semantic drawing -> IR compilation and preview
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

FX and spot-metal candles use Dukascopy's public, broker-native BID feed. It
requires no API key. Intraday candles are deterministically aggregated from
the venue's one-minute BI5 artifacts; daily candles use its annual BI5 files.
The existing `DUKAS:*` symbol IDs remain unchanged.

The feed can be used directly by a local Assay server. Publicly republishing
the candles is a separate licensing question, so R2 publication remains
blocked unless redistribution rights have been confirmed and
`ASSAY_DUKASCOPY_REDISTRIBUTION_AUTHORIZED=true` is set.

To import ten years of official BLS CPI and Employment Situation releases:

```
go run ./cmd/event-pipeline -provider bls -dir event-data -build-version v1
```

The checked-in curated catalogue remains available only for offline fixtures:
`-provider curated`.

The **Setup Test** chart lives in the Assay UI (`assay-ui`, the `/setup`
screen) and talks to this server over the `/api/setup/*` endpoints. It lets a
trader label and test break-and-retest, order-block, and breaker-block drawings
without treating the drawing's absolute price as a historical rule. This server
serves no interface of its own. See
[the Setup Test contract](docs/setup-test.md).

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

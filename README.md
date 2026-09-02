# Assay

A no-code, multi-asset strategy backtester whose defining feature is that its
numbers are statistically defensible: walk-forward by default, outcome ranges
instead of assumed fills, sensitivity made visible, and multiple testing counted
and disclosed.

Go engine, ConnectRPC transport, local web UI, prebuilt bar library on object
storage. Runs entirely on the user's machine.

## Layout

The Go module stays at the repository root so import paths remain stable; the
browser application is a sibling in `web/`.

```
cmd/assay              user-facing binary: serves the API on localhost
cmd/pipeline           bar library builder, runs on a schedule, ships to nobody
cmd/event-pipeline     imports official event releases into local snapshots

internal/core          fixed-point prices, time, timeframes, series, bitsets
internal/instrument    instrument specs, calendars, cost models
internal/store         Parquet bar format, manifests, local cache
internal/event         immutable structured-event snapshots and query model
internal/eventsource   reviewed event-source adapters (curated catalogue first)
internal/eventpipeline imports event sources into local snapshots
internal/eventstudy    event-anchored study execution
internal/library       library fetch + direct-from-venue fallback
internal/venue/*       per-venue download and decode
internal/primitive     primitive interface, registry, memoisation cache
internal/packs/*       indicator, price-action, time, SMC and semantic detectors
internal/setuptest     semantic drawing -> IR compilation and preview
internal/sim           trade simulation, costs, ambiguity bounding
internal/ir            strategy document, validation, content hashing
internal/engine        IR execution: primitives -> signals -> trades
internal/analytics     walk-forward, sweeps, Monte Carlo, sensitivity
internal/runstore      run history, trial counting, comparison
internal/rpc           ConnectRPC handlers
internal/telemetry     opt-in, local-only product metrics
internal/userdir       per-user default directories (cmd/ wiring only)

proto/assay/v1         service and primitive-parameter definitions
gen/go/assay/v1        generated Go + ConnectRPC stubs (do not edit)
web/                   browser application; generates its own TS stubs
deploy/                Dockerfiles and proxy config used by compose.yaml
docs/                  architecture, conventions, batch specs
data/                  local pipeline output (gitignored)
```

Generated code is never edited by hand: `make proto` rewrites `gen/go`, and
`npm run generate-rpc` in `web` rewrites `web/src/gen`.

Golden fixtures live beside the package that uses them, e.g.
`internal/packs/smc/testdata/golden`.

Assay writes nothing into the working directory by default. The bar cache is
regenerable and goes to the per-user cache directory (`~/Library/Caches/assay`
on macOS, `$XDG_CACHE_HOME/assay` on Linux); study history and event snapshots
must survive a cache wipe, so they go to the per-user data directory
(`~/Library/Application Support/assay`, `$XDG_DATA_HOME/assay`). Override any
of them with `-cache-dir`, `-history-dir` and `-event-dir`, or move all three at
once with `ASSAY_HOME`. Only `cmd/pipeline` writes into the repository, to the
gitignored `data/published`, and `-local-dir` moves that too.

## Getting started

```
make build      # bin/assay, bin/pipeline, bin/event-pipeline
make test       # unit and golden tests
make race       # the same suite under -race
make lint       # gofmt, go vet and golangci-lint
make bench      # hot-path benchmarks with -benchmem
```

Run the Go server from the root and the UI beside it:

```
./bin/assay
make web-install
npm --prefix web run dev
```

For a single-host deployment, copy `deploy/.env.example` to `deploy/.env`, set
your domain and email, point the domain's DNS at the host, then run
`docker compose up --build -d`. Caddy terminates TLS and serves the UI and API
through one origin; persistent state lives in the `assay-data` Docker volume.

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
go run ./cmd/event-pipeline -provider bls -build-version v1
```

The checked-in curated catalogue remains available only for offline fixtures:
`-provider curated`.

The **Setup Test** chart lives in the web client (`web/`, the `/setup`
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

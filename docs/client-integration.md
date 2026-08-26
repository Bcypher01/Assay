# Client integration

The browser calls the local Assay server through ConnectRPC; it never receives
R2 credentials or reads the bar library directly. Run `assay` with a public,
read-only R2 custom-domain/CDN URL:

```sh
assay -library-url https://data.example.com -cache-dir cache -cache-max-mb 5120 -history-dir history
```

On first launch, fetch `GET /onboarding.json` before showing a blank builder.
It returns the worked, four-year daily EUR/USD template and the exact data
window to prepare. Open that template as a normal editable strategy, then use
`EnsureData` for the returned symbol, timeframe, and range.

`DataStatus` takes a symbol, timeframe, and `[from_ms, to_ms)` window and
returns the cache-only readiness for that exact request, including missing
partitions and the snapshot hash when local data is ready. Call it before
enabling Strategy or Run workflow steps.

`EnsureData` takes the same request and streams resolution, download, and
verification progress as the server fills the local month-partitioned Parquet
cache. The cache uses a 5 GiB default oldest-file eviction budget; set
`-cache-max-mb 0` only when an unbounded cache is intentional. `Run` does not
download data. It performs a cache-only preflight and returns `DATA_MISSING` or
`DATA_PARTIAL` in `RunSummary.error_code` when the exact local dataset is not
ready.

Run responses contain `run_id`. Use it with `RunService.GetRun`; use two IDs
with `RunService.Compare`, whose response contains ordered parameter changes and
bounded metric deltas. The TypeScript client is generated under
`gen/typescript/src`; run `npm run generate` after a protobuf change.

For the result page, call `RunService.GetDetails` with the `run_id`. It returns
the persisted strategy/data identity, effective execution assumptions, bounded
in- and out-of-sample metrics, trial count, monthly/yearly P&L aggregates, and
near-miss condition counts. It intentionally returns evidence inputs rather
than a server-generated verdict, so the client can show the fixed rubric and
trace every conclusion. `RunService.GetTrades` is paginated (100 records by
default, 500 maximum) and returns entry/exit markers, P&L and cost bounds, and
the ambiguous-fill flag for the trade inspector. A retained trade list can be
evicted under history retention; in that case `GetDetails.trade_list_present`
is false and `GetTrades` returns `failed_precondition`.

Every persisted local study also includes the default Phase 4 robustness pass:
anchored held-out windows, 1×/2×/3× cost stress, a 3×3 cost/risk sensitivity
grid, and a reproducible 1,000-sample block-bootstrap. These are returned from
`GetDetails` as `walk_forward`, `cost_stress`, `sensitivity`, and
`monte_carlo`. Render the returned next-action cards as experiments: running an
altered study creates a distinct configuration and is counted in `trials`.

Interactive API documentation is available from the local server at `/docs`.
The corresponding OpenAPI 3.1 document is `/openapi.json`. Streaming endpoints
are documented there but should be called through the generated ConnectRPC
client, which handles the streaming protocol.

## Account-value display contract

All money returned for a run is an integer number of **USD cents**. This
includes streamed `EquityChunk.values`, `RunSummary.net_pnl`, trade `net_pnl`
/ `costs`, and all persisted P&L and drawdown metrics. Format them as USD
(`value / 100`) and label the curve **Balance (USD)**; never render them as
points or instrument prices. `StudyAssumptions.initial_equity` is the one
exception: it is a positive whole USD amount, so `50` means **$50.00**.

The service values every currently listed instrument in USD: EUR/USD, GBP/USD,
XAU/USD and USDT crypto use their quoted value (USDT is treated at USD parity);
USD/JPY converts realized JPY P&L at the trade's closing USD/JPY price. Send a
positive whole-dollar `initial_equity`—`100` means **$100.00**. Pair it with a
non-zero `risk_per_trade` (for example `0.01` for 1%) so lot size compounds
with the balance. Do not rely on `fixed_lots` for small accounts unless the
user explicitly chose that exposure.

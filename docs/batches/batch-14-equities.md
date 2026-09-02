# Batch 14 — Equities pathway: US first, Africa honestly

**Depends on:** 01 (bar storage), 02 (venue adapters), 04 (trade simulator),
10 (analytics)
**Blocks:** nothing; 12 packaging must ship whatever exists when it runs
**Packages:** `internal/instrument`, `internal/corpaction` (new),
`internal/venue/yahoo`, `internal/venue/stooq`, `internal/venue/csvimport`
(new), `internal/sim`, `internal/analytics`

## Goal

Make a share of stock a first-class instrument, correct enough for the case the
product is weakest at today: someone holding a position for months or years,
where dividends, splits and currency dominate the result and a one-tick
slippage assumption does not.

Coverage is US-first because that is where free, no-key, decades-deep data
genuinely exists. African coverage — Nigeria in particular — is delivered in
tiers, and the tiers exist because the data does not: the honest product says
what it can and cannot test rather than shipping a Nigerian backtest built on
three years of scraped closes.

## Current state

`ClassEquity` exists in `internal/instrument/spec.go` and nothing uses it. All
eleven shipped specs are fx, crypto or metal. Both free venue clients are
FX-only by construction: `Name()` returns `yahoo-fx` and `stooq-fx`, and both
refuse anything but D1 and W1. There is no representation anywhere in the
repository for a split, a dividend, a delisting or a ticker change.

So this is a new vertical, not a symbol-list addition.

## Deliverables

### `internal/corpaction` (new)

The package that does not exist and without which none of the rest is true.

- `Action`: kind (split, reverse split, cash dividend, special dividend,
  ticker change, delisting), effective date (ex-date, not pay-date), exact
  ratio or amount as a decimal string, source, and the snapshot it came from
- Snapshot store mirroring `internal/event`: immutable, versioned, content
  hashed. A study pins the snapshot it ran against. A vendor silently
  restating a 2014 split must not change a saved study's numbers
- `Timeline(sym, from, to)` — the ordered action set for a symbol
- Adjustment factors derived from actions, never read from a vendor column

### `internal/instrument`

- Equity spec fields: `LotStep` = 1 share (US) or board lot (NGX),
  `ShortAvailable bool`, `BorrowBps` (annualised, non-zero default),
  `DividendWithholdingBps`, `DailyPriceLimitBps` (0 = none),
  `AdjustmentPolicy`
- Calendars: `XNYS`/`XNAS` with the US holiday table and the 13:00 ET early
  closes; `XNSA` (NGX, 10:00–14:30 WAT); `XJSE`; `XCAI`. Early closes are
  windows, not holidays — the existing `Window` type already expresses this
- Seed spec set: ~500 US names plus the liquid ETFs, then tier-A African
  names (below)

### `internal/venue/yahoo`

- Generalise past FX: rename to `yahoo`, allow equity symbols, keep D1/W1 and
  add H1 where the endpoint serves it
- Request `events=div%2Csplit` and decode both into `corpaction` records in
  the same pass as the bars. This is the whole reason Yahoo is the primary
  equity source: splits and dividends, free, no key, decades deep
- Ignore `adjclose` entirely. It is a derived column that moves under you

### `internal/venue/stooq`

- US equity daily as an independent cross-check, not a primary. Two free
  sources disagreeing on a bar is information the pipeline should surface

### `internal/venue/csvimport` (new)

- Import OHLCV from a user-supplied CSV under a declared provenance record:
  source name, licence claim, date the file was obtained
- The provenance rides with every study run and appears in the report. A
  result computed from an unattributed spreadsheet is labelled as such
- This is the only honest route to NGX history today (see decisions)

### `internal/sim`

- Split handling on ex-date: position size and open stop/target levels scale
  by the ratio. A 4:1 split that silently quadruples a stop distance is a
  wrong backtest that looks fine
- Dividend crediting on ex-date, net of `DividendWithholdingBps`, as cash to
  the account — not as a price adjustment
- Short side: dividend paid out rather than received, plus `BorrowBps`
  accruing daily
- Daily price limit: an order that would fill beyond the band does not fill
- Zero-volume bar: no fill, at all. Half the NGX board is untradeable on a
  given day and pretending otherwise is where African backtests become fiction
- Participation cap: a fill larger than a configured share of the bar's volume
  is refused or partially filled

### `internal/analytics`

- Total-return equity curve from raw fills plus dividend cash, reported in the
  settle currency
- A USD view for non-USD instruments only when an FX series covering the same
  span is loaded; otherwise the option is disabled rather than approximated
- Two new run-level disclosures, alongside the existing multiple-testing count:
  **survivorship** and **short history** (below)

## Key decisions

**Store raw prices and actions; never store an adjusted series.** Vendor
adjusted closes are recomputed on every new dividend, so the same query returns
different history next month, and a pinned study stops reproducing. Assay keeps
what the tape said and the action list beside it.

**Do not back-adjust the price series at all.** Back-adjusted prices are not
multiples of the tick size, and `Spec.PriceFromDecimal` correctly refuses those.
Rather than inventing a second, looser price lattice for equities, the simulator
trades raw prices and credits dividends as cash. This produces the same total
return as an adjusted series, keeps every fill on a price that actually traded,
and keeps one price type across the whole codebase. Adjustment factors are
computed only where a *ratio* is genuinely needed — indicator inputs across a
split boundary, and chart display.

**A long backtest on today's index membership is survivorship-biased and must
say so.** No free source carries delisted tickers. Testing "the S&P 500" over
ten years using the current 500 names is a well-known way to manufacture a good
result. Assay cannot fix this on free data, so it reports it: any equity run
whose span predates the instrument set's construction date carries a
survivorship disclosure in the report, worded plainly. This is the same posture
as multiple-testing disclosure and belongs in the same place.

**Nigeria: NGX's own end-of-day history is a paid product and there is no free
public API.** NGX sells EOD back to 1996 on request. Yahoo does not carry NGX
listings at all. The free surfaces that exist are third-party aggregators of
unclear licence and unclear correctness — exactly the kind of source this
project was built to refuse. So NGX enters by two doors, both labelled:

1. **CSV import** — a user who has obtained history (from NGX, from their
   broker, from a data vendor they pay) imports it with provenance attached.
   Their data, their licence, their study.
2. **Forward capture** — an optional local job that records the daily price
   list from the day the user turns it on. History begins now and lengthens.
   Useful in two years, useless today, and honest about which.

Assay ships no NGX bar library and republishes no NGX prices.

**Tier-A African coverage is what free data actually supports.** JSE via Yahoo's
`.JO` suffix and EGX via `.CA` are genuinely covered, decades deep, with
dividends and splits. Nigerian *exposure* is reachable through the dual- and
foreign-listed names and the US-listed Nigeria ETF. That set ships in the seed
specs. NGX-local tickers do not, and the UI says why rather than showing an
empty search.

**Any equity study shorter than the walk-forward minimum is refused, not
degraded.** Short history is the failure mode the Nigeria path invites. If the
imported span cannot support the configured folds, the run does not produce a
number with a caveat attached — it does not produce a number.

**Currency is reported, never converted silently.** A naira equity curve across
the 2023 float and a dollar one tell opposite stories about the same trades.
Both are true; presenting one without naming it is not.

**Withholding tax defaults are non-zero.** Same reasoning as slippage: a zero
default is a lie most users will never notice and never change. NGX 10%, JSE
20%, US per the spec's stated assumption.

## Interfaces

```go
// internal/corpaction
type Kind uint8

const (
    Split Kind = iota
    ReverseSplit
    CashDividend
    SpecialDividend
    TickerChange
    Delisting
)

type Action struct {
    Symbol   string
    Kind     Kind
    ExDate   core.Time
    Ratio    string // "4:1" for splits, empty otherwise
    Amount   string // decimal, in QuoteCurrency, for dividends
    NewSym   string // ticker changes only
    Snapshot string // content hash of the snapshot this came from
}

type Store interface {
    Timeline(ctx context.Context, sym string, from, to core.Time) ([]Action, error)
    SnapshotID(ctx context.Context) (string, error)
}

// Factor is the multiplicative ratio adjustment between two instants,
// splits only. Dividends are cash and never enter here.
func Factor(actions []Action, at, ref core.Time) core.Ratio
```

```go
// internal/venue — unchanged Fetcher, plus an optional capability
type ActionFetcher interface {
    Actions(ctx context.Context, sym string, from, to core.Time) ([]corpaction.Action, error)
}
```

## Slices

Land in this order; each is independently useful.

| # | Slice | Ends with |
|---|---|---|
| 14a | `corpaction` package, snapshot store, golden fixtures | A split and a dividend round-trip through the store |
| 14b | Yahoo generalisation, equity specs, XNYS calendar | AAPL D1 loads with its split and dividend timeline |
| 14c | Sim: splits, dividend cash, borrow, zero-volume, price limits | A 10-year AAPL hold matches a hand-computed total return |
| 14d | Analytics: total return, survivorship and short-history disclosure | A survivorship-biased run says so in its report |
| 14e | Tier-A African specs, JSE/EGX calendars, non-USD settlement | An NPN.JO test reports in ZAR |
| 14f | CSV import with provenance, NGX forward capture | An imported NGX file runs, labelled, and is refused when too short |

## Tests

- Golden decode fixtures for a Yahoo response carrying both event types, with
  a real captured payload in `testdata/`
- A hand-computed total return for one US name across a known split and a
  known dividend, to the tick
- Split-date invariance: position value immediately before and after a 4:1
  split is equal, and stop distance in percent is unchanged
- Short-side dividend: the payment is debited, not credited
- Zero-volume and limit-band bars produce no fills
- Calendar: US early closes, NGX 10:00–14:30 WAT, DST boundaries on both
- Snapshot pinning: a study re-run against an older action snapshot reproduces
  its original numbers exactly after a newer snapshot lands
- Currency: no code path converts between currencies without an FX series

## Exit criteria

- [ ] A 10-year US equity total-return test reconciles to a hand-computed
      figure to the tick
- [ ] No adjusted price is ever persisted or transmitted
- [ ] Every equity run carries an explicit survivorship disclosure
- [ ] A study pinned to an action snapshot reproduces after a newer snapshot
- [ ] Splits adjust open orders and position size, proven by test
- [ ] Zero-volume and limit-locked bars are unfillable
- [ ] Tier-A African instruments load and report in their own currency
- [ ] An NGX CSV import carries provenance into the report, and a too-short
      span is refused rather than caveated
- [ ] Assay publishes no NGX bars to the library

## Out of scope

- Intraday equity bars. Free intraday history is capped at weeks to months,
  which cannot support the walk-forward defaults. Day traders are a later
  batch with a different data answer, and the product should say so rather
  than shipping a 60-day backtest.
- Fundamentals, earnings-date anchoring (that is the `eventstudy` path),
  options, futures roll, and index reconstitution.
- Portfolio-level backtests across many symbols at once. Single-instrument
  first; the sim's cash model needs work before a portfolio is honest.
- Any republication of third-party equity prices.

## Decision log

- **Open — verify before writing the adapter.** Confirm empirically that the
  Yahoo chart endpoint still serves `events=div,split` for equities without a
  crumb/cookie, at what rate limit, and how far back per interval. The
  container and the local shell both had no egress to that host during
  drafting, so this is unverified. Batch 02's Dukascopy precedent applies:
  answer it in writing here before the adapter is written.
- **Open.** Whether Stooq's US equity CSVs are split-adjusted in place. If
  they are, they are a weak cross-check for levels and useful only for shape.
- **Open.** Whether the NGX daily price list is published at a stable URL in a
  machine-readable form, which decides whether forward capture is a scraper or
  a download.
- **Decided.** NGX will not be represented as a supported venue in the seed
  specs until one of the two doors above is working end to end.

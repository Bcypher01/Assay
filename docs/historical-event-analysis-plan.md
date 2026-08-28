# Historical News & Major-Event Analysis Plan

## Objective

Add a reproducible research path that answers:

- How did an instrument behave around a class of major events?
- Did a strategy's outcomes materially differ during those events?
- Can the user safely exclude a defined event window from a backtest?

The feature must preserve Assay's core guarantees: no look-ahead, deterministic outputs, pinned data snapshots, and uncertainty shown rather than hidden.

## Product scope

Start with structured events, not unbounded news sentiment.

| Release | Included | Deferred |
|---|---|---|
| V1 | Central-bank decisions, CPI, employment reports, elections, manually curated market shocks | Headline sentiment, LLM classifications, live alerts |
| V2 | Actual/forecast/previous values, surprise buckets, event-to-instrument relevance | Full-text news archives and automated entity mapping |
| V3 | Optional event-window exclusion in strategies | Trading signals directly generated from news |

This keeps the first release credible and explainable. "Major event" is a controlled taxonomy, not an opaque model judgement.

## Architecture

Introduce a new, parallel historical-data domain:

\`\`\`text
event-source data
  -> event pipeline
  -> immutable event snapshots + manifests
  -> local event cache
  -> event library/query service
  -> event studies in analytics
  -> RPC/UI explorer

optional later:
event library -> event-window primitive -> engine signal arrays -> simulator
\`\`\`

Do not put event lookup logic into \`internal/sim\`. The simulator should continue to consume only signals and OHLC. Event detection/filtering belongs in a primitive or engine-adjacent data adapter; aggregate research belongs in analytics.

Suggested packages:

\`\`\`text
internal/event/
  types.go          Event, EventKind, Impact, Outcome, EventSnapshot
  taxonomy.go       controlled categories, country/currency mappings
  validate.go       timestamp and as-known-at-time validation
  store.go          immutable local snapshot storage and manifests
  query.go          deterministic filtering and range lookup

internal/eventsource/
  provider.go       ingestion interface
  curated/          checked-in/manual major-event catalogue
  calendar/         economic-calendar provider adapter, later

internal/eventstudy/
  study.go          study request/result types
  align.go          event-to-bar alignment
  returns.go        forward returns, MFE/MAE, volatility measures
  aggregate.go      cohorts, medians, distributions, confidence intervals
  strategy.go       trade/event overlap analysis
\`\`\`

Use a new top-level \`internal/eventstudy\` package rather than extending the existing strategy-optimisation implementation until its public concepts are stable. \`internal/analytics\` can orchestrate it later, just as it orchestrates repeated engine runs.

## Canonical event model

Each event should include enough information to reproduce what was knowable at the time:

\`\`\`go
type Event struct {
    ID           string       // stable, provider-independent ID
    OccurredAt   core.Time    // UTC, exact release time where known
    Kind         EventKind    // US_CPI, FOMC_RATE_DECISION, ELECTION, etc.
    Title        string
    Jurisdiction string       // e.g. US
    Currencies   []string     // e.g. USD
    Impact       Impact       // high / medium / low
    Status       EventStatus  // scheduled / released / revised / estimated
    Actual       *Decimal
    Forecast     *Decimal
    Previous     *Decimal
    RevisionOf   string       // prior event/release, if applicable
    KnowledgeAt  core.Time    // when this record became available
    Source       SourceRef
    Evidence     []EvidenceRef
}
\`\`\`

Important rules:

- \`OccurredAt\` is the time the market could react, not the day the event was later reported.
- An economic release's first-published value is distinct from later revisions.
- Events with uncertain timing need an explicit precision flag (\`exact\`, \`day\`, \`month\`); they must not be silently aligned to an invented intraday timestamp.
- The imported snapshot receives a content hash and immutable manifest, exactly like market-bar data.
- Analysis results record both bar-data and event-data snapshot hashes.

## Taxonomy and relevance

Define a small, versioned initial taxonomy:

- Monetary policy: rate decision, guidance, minutes, intervention
- Inflation: CPI, PPI, PCE
- Labour: payrolls, unemployment, wages
- Growth: GDP, PMI, retail sales
- Political/geopolitical: election, referendum, conflict escalation, sanctions
- Market stress: bank failure, exchange failure, emergency policy action
- Corporate events, later: earnings, guidance, dividends, splits

Each class has a default relevance mapping. For example, US CPI applies directly to USD pairs, broadly to gold and US equity indices, and indirectly to BTC. The UI should show this mapping and let users override it; it should never claim universal causality.

## Event-study methodology

For each selected event cohort:

1. Select event snapshot, instruments, date range, event categories, impact threshold, and analysis windows.
2. Exclude events that cannot be aligned honestly to the selected bar resolution.
3. Align each event to the first tradable bar after the event became known.
4. Calculate pre-event and forward-window measures.
5. Aggregate every eligible event in deterministic chronological order.
6. Report the sample count, excluded-event count, snapshot hashes, and methodological assumptions alongside results.

Initial measures:

- Return from event time through \`5m\`, \`15m\`, \`1h\`, \`4h\`, \`1d\`, and \`5d\`
- Absolute move and realized volatility
- Maximum favourable/adverse excursion within each window
- Gap and spread-sensitive flags
- Directional hit rate
- Median, mean, percentiles, and distribution
- Year-by-year breakdown
- Event-level drill-down with the exact bar timestamps used

Avoid causal language. The product should say "observed historical behaviour around selected events," never "this event caused the move."

## Strategy-impact study

The first strategy integration should be analytical, not a new entry signal.

For any completed backtest:

- Identify trades that were open, entered, or exited within a selected event window.
- Compare their bounded P&L, expectancy, win rate, drawdown contribution, and holding time with all other trades.
- Show results by event type, impact level, currency/jurisdiction, and year.
- Preserve pessimistic-to-optimistic outcome ranges for ambiguous fills.
- Enforce minimum sample sizes and prominently show small-sample warnings.

Example output:

\`\`\`text
High-impact USD events, 2016-2026
Eligible events: 124
Trades entered +/-60m of events: 31
Expectancy: -0.18R to -0.04R
Other trades: +0.21R to +0.29R
Conclusion: insufficient evidence / apparent degradation / no material difference
\`\`\`

The conclusion must be rules-based, transparent, and conservative. It must not recommend exclusion from a strategy based on a handful of observations.

## Event-window strategy rule: later milestone

After the explorer and strategy-impact study prove useful, add an optional rule:

\`\`\`text
Do not open a new position from 30 minutes before until 60 minutes after
high-impact USD events.
\`\`\`

Implementation constraints:

- Treat it as a boolean primitive that emits an availability mask aligned to bars.
- It can only use event records whose \`KnowledgeAt\` is no later than the evaluated bar.
- It affects new entries only by default; it must not invent exit behaviour.
- It must be visible in the strategy IR, hash, run summary, trial count, and comparison view.
- Event-window parameter sweeps count as additional trials.

## Data acquisition plan

### Phase A: curated catalogue

Build a small checked-in fixture/canonical catalogue covering 20-40 indisputable events across 2008-present:

- Global Financial Crisis milestones
- COVID market shock and emergency policy moves
- Major central-bank turning points
- Selected geopolitical shocks
- A handful of elections/referendums

Purpose: establish storage, alignment, UI, and reproducibility before committing to a commercial feed.

### Phase B: economic-release provider evaluation

Run a provider spike using these selection criteria:

- Historical depth for target markets
- Original publication timestamp and timezone
- First-release actual/forecast/previous values
- Revision history
- Stable provider IDs and correction policy
- Redistribution and local-cache licensing terms
- Rate limits, bulk export, and long-term cost
- Coverage quality around DST and holidays

Do not select a vendor based only on calendar breadth. Revision-safe, timestamp-accurate history is the requirement that protects backtest integrity.

### Phase C: licensed import pipeline

Once a provider is selected:

- Implement \`eventsource.Provider\`.
- Normalize raw provider records to the canonical model.
- Preserve provider payload hash and retrieval metadata.
- Produce immutable monthly/yearly partitions and manifests.
- Add reconciliation that identifies additions, corrections, and revisions without modifying old snapshots.
- Let users choose an event snapshot explicitly, with a sensible pinned default.

### Current implementation note: local validation before R2 publishing

The initial official BLS CPI/NFP archive is intentionally retained and tested
locally first. Once its timestamps, coverage, and event-study results are
accepted, publish the immutable snapshot and manifest through the existing
Cloudflare R2 distribution path used by market bars. Store source documents
only where their redistribution terms permit it; otherwise retain their hashes,
source URLs, and retrieval metadata locally and publish the normalised snapshot
plus manifest. Do not treat a mutable `latest` object as study input: it may be
a convenience pointer only, while every study pins a content-addressed
snapshot hash.

## API and UI

Add a dedicated \`EventStudyService\`, keeping the current study API focused.

Core requests:

\`\`\`text
EnsureEventData(scope)      // availability, cache state, snapshot choices
ListEventCategories()
QueryEvents(filter)         // browse and inspect source events
RunEventStudy(request)      // price/event analysis
RunStrategyEventImpact(request)
\`\`\`

Key result fields:

\`\`\`text
eventSnapshot
barSnapshot
methodologyVersion
eligibleEvents
excludedEventsByReason
perEventObservations
windowSummaries
yearlySummaries
strategyComparison
warnings
\`\`\`

The initial UI has three surfaces:

1. Event explorer: filters, timeline, event table, and selected-instrument reaction distribution.
2. Chart overlay: event markers with category, release values, and a direct link to the evidence record.
3. Backtest result tab, "Events": strategy trade overlap and before/after comparison.

Every page should visibly show the data snapshot, period, timezone policy, sample count, and exclusions.

## Integrity controls

These are release blockers:

- No revised economic value can replace the original value in an "as-of" study.
- No event is visible to a historical strategy before its \`KnowledgeAt\`.
- Event/bar alignment decisions are explicit and testable.
- Cache and study output are byte-identical for identical input snapshots.
- Missing event data yields a warning or refusal for event-dependent rules; it never quietly means "no event."
- Results distinguish "no eligible events" from "no observed effect."
- Event filtering and categorization decisions are versioned and included in the result hash.

## Delivery phases and acceptance criteria

| Phase | Deliverable | Acceptance criteria |
|---|---|---|
| 0: Design | Event model, taxonomy v1, methodology spec, provider scorecard | Approved contracts; documented timestamp/revision and market-closure policies |
| 1: Foundation | \`internal/event\`, local immutable store, curated fixtures | Same snapshot/query is deterministic; invalid/ambiguous timestamps rejected or labelled |
| 2: Price event study | Alignment and forward-return analytics | Golden tests validate each window and market-closed alignment; excluded-event reasons returned |
| 3: Product surface | RPC endpoints and explorer/chart overlay | User can select EURUSD, US CPI, and 2016-2026 and inspect reproducible cohort results |
| 4: Strategy impact | Trade-event overlap analysis | All comparison metrics retain ambiguity bounds; minimum-sample warnings work |
| 5: Provider integration | Licensed economic calendar ingestion and snapshot publishing | Provider corrections create new snapshots; old studies remain reproducible |
| 6: Strategy filter | Optional event-window primitive and authoring control | No-look-ahead test passes; IR hash/trial count change; backtests explain blocked entries |

## Test plan

- Unit tests: taxonomy, filtering, sorting, timezone conversion, surprise calculations, overlap classification.
- Golden tests: known events against synthetic M1/M15/D1 series; exact expected alignment and window returns.
- Look-ahead tests: revised releases and delayed/unknown publication time cannot influence prior bars.
- Determinism tests: same event snapshot, strategy, bar snapshot, and seed produce byte-identical results.
- Property/fuzz tests: malformed event records, DST boundaries, duplicate records, overlapping events, empty cohorts.
- Integration tests: event cache manifests, RPC serialization, and event-study result persistence.
- Benchmarks: five years of M1 bars against several thousand events, with chart-overlay response under the existing one-second target where practical.

## Recommended first implementation slice

Build Phase 1 and Phase 2 around a curated set of high-impact USD events and EURUSD/BTCUSD. It validates the architecture and user value with no vendor or licensing dependency. The concrete first user journey should be:

> Show the distribution of EURUSD's returns one hour and one day after high-impact US CPI releases from 2016-2026, including every underlying event and the precise snapshot used.

Once that is reliable, add strategy-impact analysis before allowing event windows to change trading rules.

# Broadening trader coverage

## Why this document

The product goal is that the majority of traders can come to Assay, express the
strategy they already run — by drawing it or building it — define an entry, and
see honest results. Today Assay covers one large, specific slice of that
population very well, and misses several nameable others. This document maps
what is covered, what is not, and the order in which to close the gaps, grounded
in the primitives and engine that exist now.

What is already strong: classic technical traders (`sma`, `ema`, `rsi`, `macd`,
`stochastic`, `adx`, `atr`, `obv`, `volume`, `bollinger_*`, `donchian_*`, with
numeric thresholds, crosses and all/any/not confluence); price-action traders
(`engulfing`, `pin_bar`, `swing_high/low`, `consolidation`, `displacement`); the
SMC / smart-money crowd (`order_block`, `breaker`, `fair_value_gap`,
`liquidity_sweep`, `liquidity_pool`, `market_structure`, `premium_discount`,
`break_retest`) now as multi-timeframe ladders and as draw-your-own setups with
a lower-timeframe entry; and session traders (`session_high/low`,
`bar_of_session`). Top-down multi-timeframe workflows work across the whole app.

## The lever: the simulator already did what the author surface hid

The observation that set the priority order below. Workstreams 1, 1a and 2 have
since acted on it; this section is kept because it explains why they came first.

`internal/sim/types.go` already implemented, and the sim loop in
`internal/sim/run.go` already executed:

- **Entry order types** — `Order{Type: OrderMarket | OrderLimit | OrderStop,
  Level}`, aligned to `Signals.Enter`.
- **Partial / scale-out exits** — `TargetPlan.Close` (fraction closed at target).
- **Move stop to break-even** — `TargetPlan.MoveStopToBreakEven`.
- **Trailing stops** — `TargetPlan.TrailDistance`.

A single `TargetPlan` already supports partial-close **and** break-even **and**
trailing the remainder at once (see `realizePartial`, `MoveStopToBreakEven`,
`updateTrailing` in `internal/sim/run.go`).

But `internal/engine/engine.go` only ever emitted `sim.TargetPlan{Level: target}`
or an empty plan, and never populated `Signals.Orders`, so every simulated trade
was a market entry with one fixed target and no management. `internal/ir/strategy.go`
had no fields for any of it — `Risk` was just `Stop`, `Target`, `PerTrade`, and
`EntryTiming` was only `next_open`.

Consequence: the two highest-value workstreams below were **wiring existing
simulation into the IR and the builder**, not new simulation. That held, with one
correction — "already tested" did not: none of these paths had a test, and
writing them found a bug in each. See Workstreams 1 and 2.

## Workstreams, in priority order

### 1. Trade-management authoring — trailing, partial exits, break-even — **done**

Unlocks: nearly every serious trader manages trades, so this alone moves a large
share of the population from "can't express my exit" to "can".

Shipped as `ir.Management`, an optional block on `Risk`:

```json
"risk": {
  "stop":   {"unit": "pips", "value": 20},
  "target": {"unit": "pips", "value": 40},
  "management": {"close": 0.5, "breakEven": true, "trail": {"unit": "pips", "value": 15}}
}
```

The close fraction is decimal at the authoring boundary for the same reason a
`Distance` is; the trail carries a unit and is resolved from the actual entry
fill by the simulator, via the new `TargetPlan.TrailSpec`, exactly as
`StopSpec`/`TargetSpec` are. Absent management serialises to nothing, so every
document written before this keeps its `Strategy.Hash()`. The engine populates
the plan in `targetPlan`; the builder authors it under "Trade management" and
`describe.ts` reads it back in plain English.

Two corrections to the survey above, found while building it:

- The management path was **implemented but had no test at all** — `internal/sim`
  covered neither partials, break-even, nor trailing. It does now
  (`internal/sim/management_test.go`).
- Writing those tests surfaced a real bug: `Run` added a partial close to the
  equity curve and then **cleared it from the position**, so a partially closed
  trade's own `Pessimistic`/`Optimistic` totals omitted the realised part. Every
  per-trade statistic — win rate, expectancy, profit factor — would have
  disagreed with the equity curve. `realizePartial` now returns the outcome it
  realised, the caller applies that to equity once, and the trade keeps it.

Validation refuses management the simulator would not perform, rather than
ignoring a control the user ticked: break-even without a partial close (a full
exit leaves nothing to protect) and an ATR trail are both field errors. On ATR,
see "Known gap" below.

Design (as built): extend the v2 exit surface in `internal/ir/strategy.go`. The minimal,
sim-matching shape needs no engine simulation change because one `TargetPlan`
already carries all three behaviours:

- add an optional management block to the target — a close fraction
  (`0 < f <= 1`, default 1 = full), a `moveStopToBreakEven` flag, and a trail
  distance carrying a unit (`core.Distance`, reusing the pips/ticks/percent/ATR
  machinery the stop already uses).
- keep it backward-compatible and hash-stable: absent management ⇒ the current
  single-target document and the current `Strategy.Hash()`.

Engine wiring: in `internal/engine/engine.go`, populate `TargetPlan.Close`,
`.MoveStopToBreakEven`, and `.TrailDistance` from the new IR fields instead of
emitting a bare `{Level}`. Resolve the trail distance from the actual entry fill
the same way stop/target distances are resolved today (`StopSpec`/`TargetSpec`).

Builder: extend the risk section (main builder `web/src/routes/strategy.tsx`,
`web/src/lib/strategy.ts`, and the generated reading in
`web/src/lib/describe.ts`) with the partial fraction, a break-even toggle, and a
trail distance+unit control (the stop/target `DistanceInput` already exists to
copy).

Follow-up (needs sim work, not v1): multiple laddered partial targets per
position. `Signals.TargetPlan` is one plan per entry today; a target ladder means
a list of targets per position and sequenced realisation in `run.go`.

Effort: medium. Sim risk: low (capability existed; it is now also tested).

### 1a. ATR distances actually resolve — **done**

A silent-failure path found while building Workstream 1, and fixed with it.

`internal/sim/run.go` resolved every unit-carrying distance with
`Ticks(cfg.Spec.PipTicks, fill, 0)` — the ATR argument was always zero. A
`{"unit":"atr"}` stop or target therefore resolved to a zero distance, `sizeFor`
returned zero lots, and the entry was dropped: an **ATR-based study produced no
trades and gave no reason**, while the builder offered `× ATR` in both unit
pickers.

The fix threads a real ATR through the pipeline without breaking any layer:

- `Signals.ATR` carries the reading per entry, aligned to `Enter`'s set bits
  like `Side` and `StopLevel`. The simulator holds no indicators of its own, so
  the value has to come from the caller.
- The engine resolves it in `resolvePlan` by evaluating the registered
  `indicator/atr` primitive through the same registry, cache and alignment as
  any other node, so the number matches the detector exactly rather than
  reimplementing Wilder's average.
- A `Distance` carries no period, so `engine.ATRPeriod` commits to the standard
  14 and every surface that offers the unit now says `× ATR(14)` instead of
  letting each one assume its own.
- The reading is taken at the **signal** bar, not the fill bar, because the fill
  happens on a bar the signal cannot see.
- A rule that fires before the ATR has warmed up is reported as an
  `atr_warmup` near miss and persisted with the other diagnostics, rather than
  being sized to nothing and vanishing.

### 2. Entry order types — limit and stop entries — **done**

Unlocks: "enter at my level" and "buy the breakout beyond X" — how most traders
actually enter, versus the previous next-bar-open market fill only.

Shipped as `ir.Order`, optional on `Entries`:

```json
"entries": {
  "timing": "next_open",
  "order": {"type": "limit", "offset": {"unit": "pips", "value": 10}},
  "long": { "...": "..." }
}
```

The trigger is either a **level primitive** read at the signal bar (`"node"`) or
an **offset** from the signal bar's close (`"offset"`), never both, and a market
order may carry neither — all three are field errors rather than settings the
run quietly ignores. The offset is resolved by the simulator through the new
`Order.Spec`/`Order.Ref`, for the same reason the trail is: only the simulator
knows the tick size. Sign is derived from the order type, so a long limit rests
below the close and a long stop above it.

The order lives on `Entries` rather than on each branch, because the engine runs
one side per study; it is honoured only for schema v2, since a v1 document
hashes without `Entries` and two documents with the same identity must not run
differently.

**The honesty problem this creates, and the fix.** A resting order is good for
one bar and is then withdrawn — it does not wait for price to come back. Under
the old code an unfilled order was `continue`d away, so a limit study would have
reported "50 signals, 12 trades" with nothing to explain the gap. `RunResult`
now carries `Unfilled`, it is on the wire in `RunSummary` and `RunDetails`, and
the run view says plainly that fewer trades is the rule's own behaviour rather
than missing setups.

Builder: an entry-order control offering market, limit and stop with a
distance-and-unit offset. The level-primitive form is supported by the document,
validation and engine but is not in the guided picker, because the client has no
primitive-kind metadata to filter the node list with — worth adding when that
metadata reaches the client.

Effort: medium, as estimated. Sim risk: low (the fill logic existed and is
gap-aware); the new work was the offset resolution and the unfilled reporting.

### 3. Reference levels and VWAP — **done**

Unlocks: intraday, futures and level-based traders — previous-day/week/month
high-low (PDH/PDL/PWH/PWL), the opening range, and VWAP. `docs/multi-timeframe.md`
Section 5.1 notes most of these are nearly free now that timeframe projection has
landed: a coarse detector emitting last completed period extremes, read from a
fine entry chart, is one line of IR.

Shipped as five detectors in `internal/packs/timeofday` — the pack the skill
already designates for session and clock-anchored levels, so no new pack:

- **`period_high` / `period_low`** (`PeriodExtremeParams{lookback}`). Rather
  than six calendar-specific types, one pair reads the extreme of a completed
  bar of its **own** timeframe. Projection supplies the calendar: a D1 node read
  from an M15 chart is PDH/PDL, because a fine bar only ever sees closed coarse
  bars; on W1 the same node is PWH/PWL. There is no monthly timeframe in
  `core.TF`, so a monthly reference is not expressible yet — stated rather than
  faked.
- **`opening_range_high` / `_low`** (`OpeningRangeParams{startMinuteUtc,
  rangeMinutes}`). Invalid while the range forms — publishing a range from its
  own bars would be look-ahead — and invalid again while the next one forms, so
  no bar is ever told "this is today's range" while today's is still building. A
  window that would cross midnight is refused rather than silently wrapped.
- **`session_vwap`** (`VwapParams{startMinuteUtc, endMinuteUtc}`). Typical price
  is accumulated as `high+low+close` and divided by three only at the end, so
  the per-bar average is never rounded before it is weighted. It resets each
  session, so yesterday's average cannot leak into this morning, and a session
  whose weights would exceed `int64` publishes **nothing** for the rest of that
  session — a wrapped sum looks exactly like a price.

The FX caveat is carried: `describe.ts` appends a sentence saying the VWAP is
tick-weighted whenever the study uses `session_vwap` on a `DUKAS:` symbol,
because broker-native FX volume is a tick count rather than traded size.

**Making them reachable.** New level primitives are useless if no user can
author one, and the guided builder composes boolean nodes only. Two catalogue
templates now use them through the existing `PriceLevelPredicate` — a break of
the previous day's high (a D1 level under an M15 entry, the canonical projection
case) and an opening-range breakout — in both `internal/ir/templates.go` and the
bundled web mirror, with `describe.ts` taught to read a price predicate as a
relation ("the close crosses above the previous day's high") rather than
rendering the bare level as if it were a condition.

**Two integration gaps the unit tests could not have found**, both caught by
running the template through the real engine:

- `engine.parameterMessage` is a hand-maintained switch from proto schema name
  to message. New parameter messages were missing from it, so a document using
  one failed at run time with an error naming a proto schema rather than
  anything the user did. There is now a test asserting every primitive the
  registry serves has decodable parameters, which names the missing case.
- `TestTemplateCatalogueAndDescriptionRoundTrip` pinned the catalogue size as a
  literal `9`. It now derives the count from `ir.PopularTemplates()`, since the
  literal only recorded how many templates existed the day it was written.

**Still open:** the guided builder cannot author a price-versus-level rule by
hand — only templates emit one — because the client has no primitive-kind
metadata to filter a level picker with. That is the same missing metadata that
keeps the level form of the Workstream 2 order trigger out of the picker, so one
piece of work unblocks both.

Effort: as estimated, small per primitive. VWAP was the one with real
arithmetic, and the overflow and reset behaviour needed deciding explicitly.

### 4. Indicator long tail

Unlocks: the remaining technical-analysis population — pivot points, Supertrend,
Ichimoku, Keltner channels, CCI, Williams %R, MFI, Parabolic SAR, MA ribbons.

Design: each is a new per-bar primitive in `internal/packs/indicator` with a
`Spec`; almost all are pure calculations over the series. Add on demand, ordering
by what users actually ask for (pivots and Supertrend tend to lead).

Effort: small each, additive, low risk.

### 5. Accessibility and out-of-model styles

Lower priority, and some are explicit non-goals for the "majority" push rather
than backlog items:

- **Natural-language "Describe my own strategy"** — the still-unbuilt gallery
  card. A prose-to-document translator that emits a strategy and runs it through
  the same `Validate` the builder uses, so the rule read is the rule run. A
  distinct, larger effort; note the visual draw path (Setup Test) already covers
  "draw their strategy", so this is convenience, not a blocker.
- **Fundamental / news / event-driven** — the Event study tool
  (`internal/event`) partially serves analysis around events; a news-trigger
  strategy engine is a separate track.
- **Multi-asset, pairs / stat-arb, options, spreads** — outside the current
  single-instrument directional model. Recommend treating these as explicit
  non-goals for the near-term coverage push rather than partial builds.

## In-flight follow-ups already surfaced

- Setup Test multi-timeframe signal-to-coarse-bar mapping is best-effort
  (`detectionBarForBaseBar` in `internal/setuptest/setup.go`): it drives the
  per-match level overlays and the sample-fired `RuleMatched` flag and may be off
  by one coarse bar. It does not affect backtest metrics. Validate against real
  multi-timeframe draws and refine if the sample indicator misbehaves.
- The Guided builder "stays live for" window control shows bars plus the derived
  duration and writes a duration; decide whether to add an explicit
  value-plus-unit duration mode (like stop/target) or leave the current display.
- The "Describe my own strategy" card copy is corrected but the feature is
  unbuilt (see Workstream 5).

## Recommended sequence

1, 1a, 2 and 3 are done. Next is 4, and the primitive-kind metadata the client
needs before a level picker can exist. Workstreams 1 and 2 were largely
exposure of already-simulated engine behaviour, which is why they came first —
though both turned out to hide a reporting bug that had to be fixed with them. Workstream 3 is
cheap now that projection has landed and opens a large new segment. Workstream 4
is additive and demand-driven.

## Verification pattern (per workstream)

Reuse what worked for the multi-timeframe setup work: unit-test the IR compile
and validation (deterministic), keep the untouched path byte-identical so
`Strategy.Hash()` and saved run identity do not move, and keep Go `gofmt`-clean
and web `prettier`/`tsc`-clean. The Go engine cannot be compiled in the cloud
sandbox (no toolchain there, and the module proxy is blocked), so each change is
implemented and statically checked there, then `cmd/assay` is rebuilt on the dev
machine and the behaviour confirmed live — driving the builder UI, or the plain
`/api/setup/*` and RPC endpoints from the browser, and reading back the compiled
document and results.

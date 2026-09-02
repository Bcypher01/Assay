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

## The lever: the simulator already does what the author surface hides

The single most important fact for prioritisation. `internal/sim/types.go`
already implements, and the sim loop in `internal/sim/run.go` already executes:

- **Entry order types** — `Order{Type: OrderMarket | OrderLimit | OrderStop,
  Level}`, aligned to `Signals.Enter`.
- **Partial / scale-out exits** — `TargetPlan.Close` (fraction closed at target).
- **Move stop to break-even** — `TargetPlan.MoveStopToBreakEven`.
- **Trailing stops** — `TargetPlan.TrailDistance`.

A single `TargetPlan` already supports partial-close **and** break-even **and**
trailing the remainder at once (see `realizePartial`, `MoveStopToBreakEven`,
`updateTrailing` in `internal/sim/run.go`).

But `internal/engine/engine.go` only ever emits `sim.TargetPlan{Level: target}`
(around line 620) or an empty plan, and never populates `Signals.Orders`, so
every simulated trade is a market entry with one fixed target and no management.
`internal/ir/strategy.go` has no fields for any of it — `Risk` is just `Stop`,
`Target`, `PerTrade`, and `EntryTiming` is only `next_open`.

Consequence: the two highest-value workstreams below are **wiring already-tested
simulation into the IR and the builder**, not new simulation. That is where the
best return is.

## Workstreams, in priority order

### 1. Trade-management authoring — trailing, partial exits, break-even

Unlocks: nearly every serious trader manages trades, so this alone moves a large
share of the population from "can't express my exit" to "can".

Current state: fully simulated (above), fully hidden from the author.

Design: extend the v2 exit surface in `internal/ir/strategy.go`. The minimal,
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

Effort: medium. Sim risk: low (capability exists and is tested).

### 2. Entry order types — limit and stop entries

Unlocks: "enter at my level" and "buy the breakout beyond X" — how most traders
actually enter, versus the current next-bar-open market fill only.

Current state: `sim.Order{Type, Level}` exists and the sim aligns and fills it;
the engine never emits it; the IR has no order concept.

Design: add an optional order to each entry branch in `internal/ir/strategy.go`
— a type (`market` default, `limit`, `stop`) and a trigger level. The level can
reference a level primitive (as `Slots.Invalidation`/`Target` already do) or be a
fixed offset from the signal bar. Validation: limit/stop require a level; a
market order forbids one. Emit `sim.Order` per entry in the engine.

Builder: an order-type control on the entry, with a level picker when not market.

Effort: medium. Sim risk: low (capability exists).

### 3. Reference levels and VWAP

Unlocks: intraday, futures and level-based traders — previous-day/week/month
high-low (PDH/PDL/PWH/PWL), the opening range, and VWAP. `docs/multi-timeframe.md`
Section 5.1 notes most of these are nearly free now that timeframe projection has
landed: a coarse detector emitting last completed period extremes, read from a
fine entry chart, is one line of IR.

Current state: none of these exist. `session_high/low` and `bar_of_session`
(the `timeofday` pack) are the closest, but there is no previous-period extreme,
no opening range, and no VWAP anywhere.

Design: new primitives, either in `internal/packs/indicator` or a new
`reference` pack, each with a `Spec` and registered in the primitive registry:
`previous_day_high` / `_low` (and week/month), `opening_range_high` / `_low`
(range of the first N minutes after a session open), and anchored VWAP
(`session_vwap`, `week_vwap`). VWAP needs a real cumulative volume-weighted
calculation and must carry the FX caveat from the doc — broker-native FX volume
is tick count, not traded size, so a VWAP on `DUKAS:*` must be labelled
tick-weighted or it silently misleads.

Effort: small per primitive (projection does the multi-timeframe work); VWAP is
the one with real calculation and a labelling caveat.

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

1 → 2 → 3 → 4. Workstreams 1 and 2 are pure exposure of already-simulated,
already-tested engine behaviour: highest return, lowest risk. Workstream 3 is
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

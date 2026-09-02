# Multi-timeframe: higher-timeframe analysis, lower-timeframe entry

Status: steps 1-3 of §6 are implemented (batch 13). Steps 4 onward are still
proposal. §3.1 was wrong and is corrected in place below.

The most common workflow among discretionary traders is a ladder: form a bias on
a coarse timeframe, locate a point of interest on a middle one, and trigger on a
fine one. Assay's IR already described this ladder; until batch 13 its engine
could not execute it. This document states the gap precisely, defines the
semantics that close it, and lists the honesty surfaces that the ladder makes
newly necessary.

---

## 1. What existed before batch 13

`ir.Strategy` carries a `Timeframes` map with `bias`, `poi` and `entry` keys, and
every `ir.Node` carries its own `Timeframe`. `Strategy.Validate` enforces that
bias is coarser than or equal to poi and to entry (`internal/ir/strategy.go`).

`primitive.Evaluate` honours a node timeframe properly: when a primitive is
`Timeframed` and the node's TF differs from the series TF, it calls
`CompletedSeries` to aggregate the entry series upward, dropping partial leading
and trailing aggregates so no unfinished bar is ever visible
(`internal/primitive/eval.go`). The detection itself is therefore already correct
and already memoised per `(pack, type, params, tf, snapshot)`.

The break is one layer up. `engine.Run` does this:

```go
if v.Len() != data.Series.Len() {
    return result, miss, fmt.Errorf(
        "engine: node %s timeframe output cannot be aligned to entry series", n.ID)
}
```

An H4 node evaluated over an M15 entry series returns roughly `n/16` values. The
engine rejects it. There is no projection from the coarse result back down to the
fine bar index, so **any strategy with a genuine ladder fails at run time**.

Two corroborating observations:

- Every template in `internal/ir/templates.go` sets `bias`, `poi` and `entry` to
  the same value, and every node's `Timeframe` to that same value. The ladder is
  expressible but has never been exercised.
- `docs/architecture.md` §"Timeframes and look-ahead" describes the engine as
  fanning closed bars upward from the finest timeframe. That describes the
  detection half, which exists. The fan *downward* — making a closed coarse
  result visible to fine bars — is the half that was never built.

So the strategy class is not missing from the model. It is missing from the
execution path, and the model's own validation rules are incomplete in ways that
only bite once the execution path exists (§4).

---

## 2. The projection rule

Add one function, at the primitive layer, beside `CompletedSeries`:

```go
// Project maps a Value computed on a coarser series onto the index space of a
// finer one by backward as-of join on close time.
func Project(v Value, from, to *core.Series, policy StalePolicy) (Value, error)
```

**The join key is close time, and the comparison is inclusive.**

For entry bar `i`, the engine's decision is taken at that bar's close,
`to.TF.CloseOf(to.Time[i])`. The visible coarse bar is

```
j(i) = max{ j : from.TF.CloseOf(from.Time[j]) <= to.TF.CloseOf(to.Time[i]) }
```

The inclusive `<=` is deliberate and is the whole subtlety. Because Assay's
timeframes nest, an H4 bar closing at 12:00 closes at the same instant as the
M15 bar spanning 11:45–12:00. That H4 close is fully determined by data at or
before 12:00, so the M15 bar at that index may see it. This is not look-ahead.

Two tempting variants are both wrong, and wrong in opposite directions:

| Variant | Effect |
|---|---|
| Join on the entry bar's **open** time | Discards the coincident-close bar. Every coarse signal arrives one entry bar late, systematically. Backtests are pessimistic and, worse, quietly different from what the trader does. |
| Strict `<` on close time | Same one-bar lag, same problem. |
| Any join that lets `j` be the coarse bar still forming | Look-ahead. Forbidden by `CompletedSeries` already, and `Project` must not reintroduce it. |

The coincident-close case is exactly the case a trader means when they say "H4
closed, now I take the M15 entry". Getting it right is the point.

**Warmup.** For entry bars before the first coarse bar closes, no `j` exists.
`Valid` is false there. It is never zero-filled.

**Staleness.** A projected value is held across the fine bars inside the coarse
bar — that is the intent, and for a level or a zone (an H4 order block seen from
M15) it is exactly right. But holding it *indefinitely* across a gap is not. A
Friday D1 bias should not silently govern Monday morning unless the author said
so. Hence `StalePolicy`:

```go
type StalePolicy struct {
    MaxAge core.Duration // 0 = one coarse period + tolerance (default)
    Hold   bool          // true = hold indefinitely, explicit opt-in
}
```

Past `MaxAge`, `Valid` goes false rather than the value silently persisting. Gaps
across weekends, holidays and halts then show up as invalid bias rather than as
stale bias, and the run report can count them.

**Determinism.** `Project` is a pure function of the coarse `Value` and the fine
`Time` array. No maps, no clock, one forward scan, O(n). It slots into the
existing memoisation without ceremony: cache on
`(coarse key, entry TF, snapshot, policy)`.

---

## 3. What projection breaks, and must fix at the same time

Projection is cheap. The semantics it disturbs are not, and shipping the join
without these three changes would produce a backtester that runs multi-timeframe
strategies and gets them subtly wrong — the worst possible outcome for this
product.

### 3.1 Cross detection — the concern, and why it turned out not to apply

*Corrected after implementation. The original claim is kept because the
reasoning that replaced it is the useful part.*

The concern was this. `engine.holdsExpression` computes `crosses_above` by
comparing `bar-1` to `bar` on the value array. On a *projected* array,
consecutive fine bars are equal except at the step, so a cross can only be
detected at the step — and the step was assumed to land on the first fine bar
of the new coarse bar, one bar after the coarse close that produced it.

It does not. Under the inclusive join of §2, the entry bar that first sees
coarse bar `j` is the one closing at the same instant `j` closes — the last
fine bar *inside* `j`, not the first fine bar after it. So the step lands
exactly on the coarse close, the `bar-1` comparison reads the previous coarse
value, and the cross fires at the right instant with no special case.
`TestProjectedCrossFiresOnTheCoarseClose` pins this.

The rule the section proposed — reject a cross-style comparison on a node whose
timeframe differs from the entry timeframe — would also have been wrong, and
expensively so. A `PriceLevelPredicate` cross is *entry price crossing a
higher-timeframe level*: M15 price crossing yesterday's high. Mixing timeframes
is the entire intent, and it is the single most common ladder in retail
trading. Forbidding it would have removed the feature this document exists to
add.

What survives is the underlying principle, which the inclusive join happens to
satisfy rather than a patch having to enforce: **a temporal operator on a node
must see that node's own timeframe**. Where that stops being automatic — an
operator whose result depends on more than the adjacent pair of values — it
needs the treatment §3.2 gives windows.

### 3.2 Window operators must be expressed in time, not bars

`Within{Bars}`, `Persists{Bars}`, `After{WithinBars}` and the v1
`Trigger.WithinBars` all count entry-series indices. Once nodes live on different
timeframes, "within 3 bars of the H4 POI" is genuinely ambiguous — three H4 bars
(12 hours) or three M15 bars (45 minutes)? These differ by 16x and both readings
are what someone meant.

Store these windows as **durations** in the IR and resolve them against bar close
times at evaluation. The UI keeps showing "3 bars" in whichever timeframe the
author is looking at, and converts on the way in. This kills the ambiguity
permanently and, as a bonus, behaves correctly across weekend gaps, where a bar
count silently means something different.

This is a breaking IR change. It is much cheaper now than after users have saved
ladder strategies.

### 3.3 Validation is currently incomplete

`Strategy.Validate` checks bias ⊒ poi and bias ⊒ entry. It does not check:

- **poi ⊒ entry** — nothing stops an M15 POI under an M5 entry today.
- **node timeframe ⊒ entry timeframe** — a node coarser than entry projects; a
  node *finer* than entry cannot be aggregated from the entry series at all and
  must be refused, not degraded. `docs/architecture.md` already states this rule
  for base resolution ("An M5 entry on M15 bars is refused, not degraded"); the
  same rule needs to apply per node.
- **the data request must derive from the finest node timeframe**, not from
  `Timeframes["entry"]` as `internal/rpc/local.go` does today. If the two are
  kept consistent by validation, this is a non-issue — but it should be enforced
  in one place rather than assumed in two.

---

## 4. What the ladder does to the statistics

This is the part that matters more than the plumbing, because it is where the
product's claim lives. A ladder does not just add a feature; it changes what the
resulting numbers mean, and every one of these changes flatters the strategy if
left unreported.

**Effective sample size collapses.** A D1 bias with M15 entries gives up to 96
trigger opportunities per bias observation. If a study produces 300 trades, those
trades may span only 40 distinct daily bias regimes. Forty is the number that
governs how much the bias rule has actually been tested; three hundred is the
number a naive report shows. **Report both**: trades, and distinct coarse-bar
regimes the trades fall under. A large ratio between them is the single loudest
signal that a multi-timeframe result is weaker than it looks.

**Multiple testing is amplified silently.** `internal/runstore` already counts
trials. Adding a ladder multiplies the search space by the number of admissible
timeframe combinations, and a trader who tries D1/H4/M15 then D1/H1/M5 has run
two trials that the trial counter should see as such. Timeframe selection is a
hyperparameter and belongs in the count.

**Ladder sensitivity is the natural robustness test.** Because primitives are
memoised per timeframe, re-running with the bias one step coarser and one step
finer is nearly free. A result that survives D1 bias but dies at H4 and W1 is fit
to the ladder, not to the market. This slots directly into the existing
sensitivity analytics and is, as far as I can tell, not something the mainstream
tools surface.

**The lag ablation is a look-ahead smoke test.** Re-run with every projected
value delayed by one coarse bar. A strategy that collapses under that delay is
living entirely on the coincident-close boundary — it depends on acting at the
same instant the coarse bar closes, with zero tolerance for the data latency any
real execution has. Reporting the paired result ("expectancy 0.22R; with one-bar
coarse lag, 0.03R") tells the user something no other backtester tells them, and
it costs one extra run.

**Bias-conditional breakdown.** Group trade outcomes by coarse bar so the user
can see whether the edge is spread across regimes or concentrated in a handful of
them. Cheap, and it directly serves the "one historical sample" framing.

**Run header.** State the ladder plainly — `bias D1 · POI H4 · entry M15`,
`96 entry bars per bias bar`, `41 distinct bias regimes` — next to the headline
number, not in a details pane.

---

## 5. Strategy families the ladder unlocks

The ladder is not one feature; it is the precondition for most of the workflows
retail traders currently run. Assay's packs already cover a lot of the vocabulary
— `order_block`, `breaker`, `fair_value_gap`, `liquidity_pool`,
`liquidity_sweep`, `market_structure`, `displacement`, `premium_discount`,
`break_retest`, plus the `timeofday` session detectors. What follows is what is
*not* covered, grouped by the mechanism each family needs, because the grouping
is the actionable part: three of these four groups need engine work, not more
detectors.

### 5.1 Group A — pure coarse levels (free once projection lands)

These need nothing beyond §2. They are single levels or zones detected on a
coarse series and held across fine bars, which is exactly what `Project` does.

- **Previous day / week / month high and low** (PDH, PDL, PWH, PWL). The single
  most widely used multi-timeframe reference in retail trading, and absent from
  the packs. It is the canonical projection case: a D1 detector emitting last
  completed day's extremes, read from M15. Ship it as the first ladder template —
  it demonstrates the mechanism in one line of IR.
- **Opening-range breakout.** `session_high` / `session_low` plus
  `bar_of_session` almost get there, but the popular formulation is "the range of
  the first N minutes after the session open", which needs an explicit
  opening-range primitive rather than a whole-session extreme.
- **Anchored VWAP.** `obv` exists; there is no VWAP at all. Session-anchored and
  week-anchored VWAP are heavily used as coarse reference levels, and anchoring
  is a timeframe choice, so it belongs here rather than in the indicator pack as
  a plain rolling calculation. Caveat: FX volume from a broker-native feed is
  tick count, not traded size, so a VWAP on `DUKAS:*` symbols must be labelled as
  tick-weighted or it will quietly mislead.
- **Daily/weekly opening gap.** Gap-fill rules are a coarse-open reference read
  from a fine series.
- **Inversion FVG.** An FVG traded decisively through becomes an opposing zone —
  the FVG analogue of what `breaker` already does for order blocks. The
  asymmetry is odd: the pack has the order-block inversion but not the gap one.

### 5.2 Group B — directional state machines (engine work)

These carry state across bars per side, which the current per-bar `Value` model
handles only awkwardly.

- **BOS versus CHoCH.** `market_structure_bullish` / `_bearish` exist, but a
  break of structure (continuation) and a change of character (reversal) are the
  same geometric event distinguished only by the trend state in force when it
  fires. Every SMC entry model turns on that distinction, and it cannot be
  computed from one bar's neighbourhood — it needs a state machine seeded by the
  prior structural leg. Under a ladder it matters twice over: the coarse CHoCH is
  the bias flip, and the fine CHoCH is the trigger, and they are the same
  primitive at two timeframes.
- **The sweep-then-shift model.** Coarse liquidity sweep, then a fine change of
  character, then entry on the fine order block or FVG left by the displacement
  leg. This is the dominant retail model and the clearest single argument for
  building the ladder. `NewBitcoinSweepShift` gestures at it but pins every node
  to H1.
- **Judas swing / Asian-range raid.** The Asian session range is taken out early
  in London or New York, then price reverses. Needs the session range as a
  persistent zone plus a raid-and-reclaim state, not a per-bar boolean.

### 5.3 Group C — the forming-bar carve-out (a real design tension)

**Power of Three / AMD (accumulation, manipulation, distribution)** models the
shape of a *forming* coarse candle: price opens, sweeps against the intended
direction, then expands. Entries are taken relative to the coarse bar's **open**
while that bar is still forming.

This looks like it violates the no-look-ahead rule, and it does not. A bar's open
is fixed at the instant the bar begins; its high, low and close are not known
until it closes. `CompletedSeries` currently drops the forming bar wholesale,
which correctly excludes the unknowable three fields but also discards the one
field that is legitimately known.

If this family is to be supported, the carve-out must be explicit and narrow: a
`CoarseOpen` accessor that exposes only `Open` and only for the coarse bar
containing the current fine bar, with no path to `High`, `Low`, `Close` or
`Volume`. Anything looser reintroduces exactly the leak the architecture is built
to prevent. My recommendation is to build it, because AMD is popular enough that
users will otherwise fake it badly — but to build it as a separate, obviously
named accessor rather than by relaxing `CompletedSeries`.

### 5.4 Group D — multi-symbol (a larger change)

**SMT divergence** — one correlated instrument makes a higher high while its pair
does not (ES/NQ, EURUSD/DXY, gold/silver) — is genuinely popular and genuinely
out of reach: `engine.Run` takes a single `*core.Series`. Supporting it means a
symbol-keyed series map, a cross-symbol timestamp alignment rule with its own
as-of semantics, and a decision about what to do when one venue has a bar the
other does not. It is a batch of its own, not a detector, and it should not be
smuggled into the ladder work. Worth noting in the roadmap so the IR's `Universe`
field is not designed in a way that forecloses it.

### 5.5 A composition operator, not a detector

**Confluence** — the Unicorn model (a breaker and an FVG overlapping), or any
"coarse zone and fine zone intersect" rule — is not a new primitive. It is a
zone-intersection operator over two existing zone-valued nodes, and under a
ladder the two nodes will usually sit on different timeframes. Adding
`zone_overlaps(a, b)` to the expression grammar covers Unicorn and an open-ended
set of user-invented confluence rules with one change, and avoids a combinatorial
pack of named setups.

This is worth stating as policy: **prefer composition operators over named
setups.** Every named setup shipped is a rule Assay endorses; every operator
shipped is a rule the user authors and Assay merely measures. The second is the
honest posture and the smaller maintenance surface.

### 5.6 What the honesty surfaces owe these families

Two additions to §4 fall out of this list specifically.

Most of these models are defined on a *sequence* — sweep, then shift, then
retrace, then entry — so the near-miss reporting (`engine.NearMiss`) becomes far
more valuable than it is for a single-condition strategy: "sweep held 412 times,
shift followed 61 times, entry filled 18 times" is the diagnostic a user actually
needs, and the funnel should be reported per stage, per timeframe.

And these models are unusually parameter-dense — displacement body ratio, swing
lookback, sweep close-back bars, expiry, mitigation rule, OTE bounds — each with a
folkloric default and no principled basis. The existing sensitivity analytics
should be pointed at them hard, because a family this over-parameterised is where
the multiple-testing disclosure earns its keep.

---

## 6. Implementation order

1. ~~`primitive.Project` + `StalePolicy`, with table tests over the full
   timeframe ladder, explicitly asserting the coincident-close case and the
   warmup case.~~ Done — batch 13.
2. ~~Engine: replace the `v.Len() != Series.Len()` rejection with a projection
   call. Determinism test: byte-identical output over two runs of a ladder
   strategy.~~ Done — batch 13.
3. ~~IR: duration-valued windows (§3.2); the poi ⊒ entry and node ⊒ entry
   validation rules (§3.3).~~ Done — batch 13. The cross rule (§3.1) turned out
   to be unnecessary; see the correction there.
4. Templates: convert at least two to genuine ladders — the SMC sweep/shift one
   is the obvious candidate (H4 bias, H1 POI, M15 trigger).
5. Golden MTF fixtures. Without annotated ladder charts, projection regressions
   are invisible.
6. Analytics: effective sample size, bias-conditional breakdown, ladder
   sensitivity sweep, lag ablation.

Then, in dependency order, the families from §5:

7. Group A detectors (§5.1) — PDH/PDL first, as the template that demonstrates
   projection; then opening range, anchored VWAP, opening gap, inversion FVG.
   These need no engine change beyond step 1.
8. `zone_overlaps` in the expression grammar (§5.5). One change, covers Unicorn
   and every user-authored confluence rule.
9. Structural state (§5.2) — BOS/CHoCH as a stateful primitive, then the
   sweep-then-shift and Judas-swing templates built on it.
10. The `CoarseOpen` carve-out (§5.3), only if AMD is judged worth it, and only
    as a narrow accessor.
11. Multi-symbol series (§5.4) — its own batch, deliberately last.

Steps 1-3 are one batch — `docs/batches/batch-13-multi-timeframe.md` — and
unblock everything else. Step 6 is a second, and it
is the one that makes the feature match the product's thesis rather than merely
matching competitors — I would not ship 7 onward without it, because the families
in §5 are precisely the ones whose parameter density makes an unqualified
backtest number misleading.

---

## 7. Related gaps this exposes

Working through the ladder surfaced three adjacent problems worth their own docs.

**Entry timing is `next_open` only.** `ir.EntryTiming` admits one value. The
whole reason a trader drops to a lower timeframe is usually to place a *resting
order at a level* — a limit at the order block, a stop above the sweep. That
cannot be expressed today, so the ladder as built would still force market
entries at the next open, which is not the workflow being modelled. Resting
orders bring their own ambiguity (a bar that gaps through the level, a bar that
touches and reverses within the same bar) — which is a good fit for the existing
ambiguity-bounding machinery rather than a reason to avoid them. For the SMC
audience this may be a larger gap than multi-timeframe itself.

**Ambiguity bounding covers exits, not entries.** `internal/sim` bounds the
stop-versus-target case. A limit fill inside a bar has exactly the same
unresolvable ordering problem, and once resting orders exist it needs the same
treatment: evaluate both ways, report a range.

**Session-anchored daily bars.** `internal/core/timeframe.go` aligns UTC-based
and notes the issue in a comment. An FX daily bar conventionally opens at 17:00
New York, not 00:00 UTC. Every daily-bias strategy is therefore currently formed
on a bar the trader does not use, and with a ladder that error propagates into
every entry underneath it. The comment already flags this; the ladder raises its
priority considerably.

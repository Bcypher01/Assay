# Batch 14 — Multi-timeframe projection

## Goal

Make the engine execute the ladder the IR has always described: form a bias on
a coarse timeframe, locate a point of interest on a middle one, trigger on a
fine one. The detection half already worked — `primitive.Evaluate` aggregates
the entry series upward and hides unfinished bars. The fan *downward*, making a
closed coarse result visible to the entry bars that follow it, did not exist,
and `engine.Run` rejected any node whose output length differed from the entry
series.

Design and rationale: `docs/multi-timeframe.md`.

## Depends on / blocks

Depends on 05 (primitive registry), 06 (base packs), 07 (IR + engine).

Blocks the honesty surfaces the ladder makes necessary (effective sample size,
bias-conditional breakdown, ladder sensitivity, lag ablation — batch 14), and
every strategy family in `docs/multi-timeframe.md` §5.

## Packages

`internal/core`, `internal/primitive`, `internal/engine`, `internal/ir`,
`internal/rpc`.

## Deliverables

1. `core.Duration` — a millisecond span with a unit-suffixed serialised form.
2. `primitive.Project` and `primitive.StalePolicy` — a backward as-of join on
   close time mapping a coarse `Value` onto a fine index space.
3. `primitive.EvaluateAligned` — evaluate then project, memoised.
4. `engine.Run` projects instead of rejecting; `engine.Data.Stale` carries the
   staleness policy.
5. IR windows are durations; `Strategy.BaseTimeframe`; the two missing
   validation rules.

## Key decisions

**The join is inclusive on close time.** For entry bar `i` the visible coarse
bar is `max{ j : from.TF.CloseOf(from.Time[j]) <= to.TF.CloseOf(to.Time[i]) }`.
Because the timeframes nest, an H4 bar closing at 12:00 closes at the same
instant as the M15 bar spanning 11:45–12:00, and that close is fully determined
by data at or before 12:00. Joining on the entry bar's open time, or comparing
strictly, delays every coarse signal by one entry bar — systematically, and in
the direction that makes the backtest differ from what the trader did. Both
`TestProjectCoincidentClose` and `TestRunSeesCoarseBarOnItsCoincidentClose`
fail under the strict variant; that was checked, not assumed.

**Staleness invalidates rather than persists.** A projected value is held
across the fine bars inside its coarse bar — that is the point. Holding it
indefinitely across a gap is not: a Friday daily bias should not silently
govern Monday morning. The default `StalePolicy` bound is one coarse period
plus one entry bar; past it `Valid` goes false, so weekend and holiday gaps
show up as invalid bias rather than as stale bias. `Hold` is an explicit
opt-in.

**Warmup is invalid, never zero-filled.** Before the first coarse bar closes
there is no value, and a zero is a value.

**Windows are durations, not bar counts.** "Within 3 bars of the H4 point of
interest" is either 12 hours or 45 minutes once two nodes sit on different
timeframes — a factor of 16, with both readings plausible. Windows resolve
against bar close times, which also makes them behave correctly across a
weekend. Documents saved with a bar count keep it, resolved against the entry
timeframe, which is what it meant when it was written; validation requires
exactly one of the two forms.

**A node finer than the entry series is refused, not degraded.** It cannot be
reconstructed from bars that do not contain it. `docs/architecture.md` already
states this for base resolution; it now applies per node.

**Cross operators need no special case, and must not be forbidden.**
`docs/multi-timeframe.md` §3.1 predicted that a cross on a projected array
would fire one bar late. It does not, because the inclusive join puts the step
on the coincident-close bar — the instant the coarse bar closed.
`TestProjectedCrossFiresOnTheCoarseClose` pins this. The rejection rule §3.1
proposed would also have broken the most useful ladder there is: entry price
crossing a higher-timeframe level, where mixing timeframes is the whole intent.

## Interfaces

```go
// internal/primitive
type StalePolicy struct {
    MaxAge core.Duration // 0 = one coarse period + one entry bar
    Hold   bool          // explicit opt-in to hold indefinitely
}
func Project(v Value, from, to *core.Series, policy StalePolicy) (Value, error)
func EvaluateAligned(ctx context.Context, p Primitive, params proto.Message,
    tf core.TF, s *core.Series, snapshot string, cache Cache,
    policy StalePolicy) (Value, []Overlay, error)

// internal/ir
func (s *Strategy) BaseTimeframe() (core.TF, bool)
```

## Tests

- `internal/core`: `Duration` string, parse and JSON round trip; a unitless
  number is refused rather than assigned a unit.
- `internal/primitive`: coincident close, warmup, staleness (default, `Hold`,
  explicit `MaxAge`), source invalidity, every `Kind`, the full M5 ladder from
  M15 to D1, refusal of a finer source, determinism, and `BenchmarkProject`
  with `-benchmem`.
- `internal/engine`: an end-to-end H4/M15 ladder run, the coincident-close
  entry bar, no entries before the first coarse close, refusal of a node finer
  than the entry series, byte-identical output over two ladder runs, duration
  windows against their bar-count equivalents, and projected cross timing.
- `internal/ir`: poi ⊒ entry, node ⊒ entry, `BaseTimeframe`, and the
  exactly-one-window-form rule.

## Exit criteria

- A strategy with genuinely different bias, POI and entry timeframes runs.
- `make lint`, `make test`, `make race` clean.
- `BenchmarkProject` allocates only its output arrays.

## Out of scope

Templates converted to genuine ladders, golden MTF fixtures, the ladder
honesty surfaces, and every strategy family in `docs/multi-timeframe.md` §5.

## Decision log

- **No new dependencies.** `core.Duration` is hand-rolled rather than
  `time.Duration`: spans are compared against `core.Time`, which is a
  millisecond count, and a nanosecond type invites silent unit errors at every
  boundary.
- **`StalePolicy` lives on `engine.Data`, not in the IR, for now.** A run is a
  pure function of `(IR document, data snapshot, seed)`, so a host-supplied
  policy that changed results would be a determinism bug. Today every caller
  uses the zero value, which is deterministic. When authors need to vary it,
  the field belongs in the IR document, not on `Data`.
- **The projection is memoised under a key derived from the detection pass's
  key**, plus target timeframe and policy — so a sweep reusing one coarse
  `Value` across configurations pays for the join once.
- **`engine.Data.Fine` is unrelated to this work.** It is the intrabar series
  the simulator uses to order stop and target touches, not a ladder rung.
- **§3.1 of the design doc was wrong about cross operators** and is corrected
  in place. See Key decisions.

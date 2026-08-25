# Batch 09 — SMC pack and golden set

**Depends on:** 05, 06
**Blocks:** nothing structurally; blocks the product's differentiation
**Packages:** `internal/packs/smc`

## Goal

Implement Smart Money Concepts primitives with every judgement call exposed as a
parameter, and build the annotated golden set that is the only way to know
whether the definitions match how traders actually draw them.

This is the highest-risk batch in the project. Not technically — the code is
straightforward — but because no consensus definition exists, and shipping the
wrong defaults means users conclude the tool is broken.

## Deliverables

| Primitive | Parameters that must be exposed |
|---|---|
| Market structure (BOS / CHoCH) | Swing fractal lookback; confirmation by close or wick; equal-level tolerance; whether internal structure is tracked separately from swing structure |
| Displacement | Body-to-range ratio or ATR multiple; minimum consecutive bars; whether a gap is required |
| Fair value gap | Minimum size in ATR or ticks; mitigation rule — first touch, 50%, or full fill; expiry in bars; whether inverted FVGs are tracked |
| Order block | Last opposing candle or last opposing cluster; refine to body or use full range; must be unmitigated; must be followed by displacement within N bars |
| Breaker | Source order block; how many touches invalidate; direction flip rule |
| Liquidity pool | Equal-high/low tolerance; minimum touches; session and daily/weekly extremes; age limit |
| Sweep | Wick-through depth threshold; bars allowed to close back inside; whether a structure shift must follow |
| Premium / discount | Dealing-range definition; fib boundaries; OTE band |

Plus the golden-set harness:

- Fixture format: instrument, timeframe, window, annotated objects with type and
  bounds
- Runner comparing detector output to annotations, reporting agreement per
  primitive and per annotator
- Disagreement report: where annotators disagree with each other, which is the
  data that tells you which parameters must exist

## Key decisions

**Ship one documented default per concept and expose every judgement call.**
Users will disagree with the defaults. The product survives that only if the
disagreement is a parameter they can change, not a definition they must accept.

**Recruit three or four practising SMC traders to annotate.** Their disagreement
with each other is the deliverable as much as their agreement. A concept where
annotators diverge is one that needs a parameter; a concept where they converge
can have a confident default.

**Detector correctness cannot be unit-tested against an oracle.** There is none.
The golden set is the substitute, and any parameter change that moves agreement
is a visible regression.

**Overlays are not optional here.** A user cannot debug an SMC strategy without
seeing what the tool detected. Every primitive in this pack ships its overlay in
the same commit as its logic.

**Internal versus swing structure is a real distinction, not a nicety.** Many
strategies are defined on internal structure inside a swing leg. Model both from
the start; bolting it on later means reworking every dependent primitive.

## Interfaces

Same `primitive.Primitive` contract as batch 06. Zone-kind outputs carry a
validity window and a mitigation state:

```go
type Zone struct {
    From, To     core.Time
    Upper, Lower core.Price
    Side         core.Side
    Mitigated    bool
    MitigatedAt  core.Time
}
```

## Tests

- Golden-set agreement above the agreed threshold at default parameters, per
  primitive
- Every disagreement at defaults is explainable by an exposed parameter — this is
  asserted by a test that sweeps the parameter and shows agreement recovering
- Mitigation state transitions: touch, 50%, full fill, each under its rule
- Expiry: a zone past its age limit stops being reported
- Determinism and benchmarks over 2.6M bars

## Exit criteria

- [ ] Golden set covers at least 200 annotated windows across at least 4
      instruments and both trending and ranging regimes
- [ ] At least 3 outside annotators contributed
- [ ] Agreement threshold met at defaults for every primitive
- [ ] Every annotator disagreement is reachable by parameter change
- [ ] Definitions documented in `docs/primitives.md` with the school they follow

## Out of scope

Strategy templates using these primitives (batch 12). Anything that assumes a
particular SMC methodology is correct — the pack is a library, not an opinion.

## Decision log

_Record the annotators, the agreement metric and threshold, and each default with its rationale._

**Recommended defaults.** The initial UI/configuration default should use symmetric swing lookback,
close confirmation, a 70% displacement body/range threshold, and a three-candle
FVG with a two-tick minimum, first-touch mitigation, and a 100-bar expiry.
These are product defaults rather than methodological claims; each is exposed
as a protobuf parameter. Local deterministic fixtures exercise mitigation and
expiry.

**External validation pending.** The required 200-window, multi-annotator
golden set and its agreement threshold cannot be fabricated from synthetic test
data. It remains a release gate until annotations and their provenance are
supplied.

**Annotation pilot started.** `internal/packs/smc/testdata/golden/pilot-fvg.json`
is the copyable fixture template. The pilot records raw precision, recall, and
F1 per primitive and annotator; it deliberately has no agreement threshold
until independent annotations establish a defensible baseline. The included
fixture is a synthetic harness check, not an annotator contribution.

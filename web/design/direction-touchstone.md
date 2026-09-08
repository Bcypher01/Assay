# Direction: Touchstone

A fourth UI direction for Assay, prototyped in
[`direction-touchstone.html`](direction-touchstone.html).

The concept: an assay is a test of what a metal is worth. Touchstone keeps the
product's own identity — the **run-log side spine** and the exact HSL colour
system shipped in `web/src/styles.css` — and layers the
[jakubkrehel/skills](https://github.com/jakubkrehel/skills) principles on top:
one accent hue used with discipline, concentric radii, shadows for depth where
borders were doing structure, a type stack that reads like a signed judgement,
motion as a single staggered language. It is the run log, sharpened.

## The palette is the one the app already ships

No new palette. Every token in the prototype is pulled from
`web/src/styles.css` and aliased to a semantic role:

| Token | Renders as | Role |
| --- | --- | --- |
| `--seal` | `--primary` in HSL | the one interactive accent — primary action per view, the wordmark mark, rule emphasis |
| `--pass` | `--success` | the evidence that held up (equity, winners, "Held") |
| `--caution` | `--warning` | the numbers that wobble (drawdown, ambiguity) |
| `--fail` | `--destructive` | the numbers that lied ("Failed", stops) |
| neutrals | `--background/card/accent/secondary/border` | surfaces, edges, text at three depths |

Dark mode keeps both existing mechanisms (`prefers-color-scheme` *and*
`data-theme` override, verbatim from the app), so system and manual theme agree
from first paint — including the toggle's own sun/moon icon.

## Concentric radius, shadows for depth

Outer radius = inner radius + its padding, so nested elements never fight:

```
--r-card  20px   cards / hero / verdict / ledger
--r-panel 14px   panels / tables / annotations
--r-field 11px   inputs / buttons / tools
--r-chip   8px   chips / badges / rule numbers
--r-seal   7px   the wordmark seal
```

Card inner hairlines (`::after` at `inset: 5px`) and button shadows use the
same concentric rule. Elevation is layered `box-shadow`; borders are reserved
for structure and selection.

## Type

- **Syne** (display) for titles and the verdict word — a decisive, geometric
  judgement face that reads clean at `800` and at small sizes alike.
- **Instrument Sans** for UI text, kept at 400/500/600 — nothing below 400 at
  UI sizes.
- **Spline Sans Mono** with `font-variant-numeric: tabular-nums` for anything
  that changes (equity, counts, stats) so digits never shift the layout.

Headings scale `50 → 44 → 24 → 19px`, `text-wrap: balance` on headings,
`line-height: 1.55–1.6` on anything that wraps to three lines.

## Motion is one language

- Enter: `translateY(10px)` + fade over `560ms` with `cubic-bezier(0.2,0,0,1)`,
  staggered ~100ms per child via `.rise > *:nth-child(n)` — one rule, applied
  wherever a screen reveals its content.
- Press: `scale(0.96)` on all buttons, `120ms`.
- Hover/state: `150ms` transitions on only the properties that change.
- Theme swap: inject `transition: none`, force a reflow, restore on the next
  frame, so the page snaps instead of smearing.
- `prefers-reduced-motion: reduce` collapses every duration to ~0.

## Shell: the run-log spine, kept

The 288px spine from `app-nav.tsx` stays, ported as-is: the teal `A` wordmark,
six steps with square markers (done = teal check, active = ring), mono summary
lines per step, Tools (Setup test / Event study), the Research trail session
card with its trial count and reset, Appearance toggle, and the honesty note
that bottoms out the column. Nothing stands between the reader and the numbers
except the spine and the ledger.

Below `880px` the spine collapses exactly as the shipped app does — a
horizontal, snap-scrolling step strip, not a hamburger.

## The verdict is the hero

The product promise is *statistically defensible numbers*, so the verdict gets
the largest type in the app: a single sentence in Syne at up to `82px`,
stamped by a rotated teal seal ("assayed · 4 yrs"), then a `ledger` strip of
five tabular figures. The ambiguity disclosure ("reported, not guessed") is
carried on a caution left rule rather than another card.

## What the run proves

The run screen draws the equity curve live over the same deterministic series
(LCG, seed 42) that ends exactly where the ledger says it does — **+8.42%** —
so the verdict and the chart cannot disagree about the headline number.

## Open questions for a build

- Syne over Instruments: keep both, or drop Instrument Sans for a neutral
  grotesque to push the display face further into relief?
- The stamp is a static SVG here; a real verdict could rotate it in on the
  hero's entrance.
- The spine step strip needs a real mobile homescreen decision (labelled steps
  vs a condensed drawer), not just truncation of the mono summaries.
- `--seal-wash` currently backs subtle washes; verify every `seal`-adjacent
  pair against `--ground` and `--surface` per the better-colors ramp rules
  before first build.
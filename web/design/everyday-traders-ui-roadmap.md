# Everyday traders UI roadmap

The product plan in the engine repository remains the cross-functional source
of truth. This document owns the front-end work that turns its requirements
into screens, copy, and interaction acceptance criteria.

## Phase 0: validate product language

Use the run-log prototype and the [validation kit](phase-0-validation.md).
Complete five observed sessions before accepting the phase. Success requires
four of five participants to explain the rules and drawdown/loss-streak
language without help, and no participant to mistake historical performance
for a forecast after reading the verdict.

## Phase 2: guided builder and data preparation

Build:

- a template gallery for the six engine-supported strategies;
- a sentence-based rule editor with a compact advanced view;
- a recent-chart condition preview and visible zero-trade diagnosis;
- Typical, Conservative, and Custom assumption controls;
- 3/5/10-year selection with the longest reliable range as default;
- a single preparation/run progress surface using trader-facing language.

Acceptance: a new user can run a valid template within three minutes and can
recreate a written strategy within ten; packs, JSON, cache partitions, and UTC
minutes never appear in the normal flow.

## Phase 3: result and evidence page

Build the verdict first, then progressively disclose answers to:

1. Would I have made money?
2. Could I have lived through it?
3. Was it consistent?
4. Is the test believable?
5. What actually happened?

The report must make assumptions, ambiguous-fill ranges, annual/monthly
breakdowns, trade inspection, and near-miss explanations discoverable without
turning into a statistics dashboard.

## Phase 4: robustness and next action

Show walk-forward, sensitivity, cost stress, Monte Carlo ranges, and trial
count as evidence behind one recommended next action. Any experiment must be
named, linked to its parent run, and shown beside the prior result; the UI must
not suggest repeated tweaking until a result becomes profitable.

## Phase 5: private beta and refinement

Add onboarding, example data, product instrumentation, failure states, and
usability fixes informed by 10–20 traders. Preserve the product limit: this is
historical evidence, not a prediction or a recommendation to go live.

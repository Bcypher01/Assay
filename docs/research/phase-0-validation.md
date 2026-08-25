# Phase 0 validation kit: everyday traders

This is the research kit for the product language validation phase in
`docs/product-plan-everyday-traders.md`. It is deliberately not a usability
result: complete the five sessions below before claiming the Phase 0 exit
criteria.

## Prototype and golden journey

Use the Phase 0 prototype supplied by the product team. It must support both
required starting paths: the 20/50 EUR/USD template and a custom strategy
description. Its figures must be illustrative research stimuli, never product
results; hypothetical-performance treatment is itself part of what the sessions
must validate. The golden journey is:

1. Choose **Popular strategy** and customize the EUR/USD daily 20/50 crossover.
2. Ask the participant to explain the long rule, short rule, order timing, and
   risk setting in their own words.
3. Select **Conservative** on the Trading reality screen. Ask what changed and
   why it matters.
4. Continue through the holdout-aware test preparation step.
5. On the verdict, ask what the drawdown range and 11-loss streak mean for a
   trader risking 1%, whether the result predicts the next trade, and what the
   recommended next action is.

## Fixed verdict rubric

| Verdict | Evidence rule | Primary next action |
| --- | --- | --- |
| Not testable | Rules are ambiguous or data is insufficient. | Clarify rules or obtain data. |
| Too little evidence | Too few independent trades or too little history. | Collect more history; do not optimize. |
| Unprofitable | Out-of-sample expectancy is negative under conservative fills. | Discard or rewrite the hypothesis. |
| Fragile | Outcome depends heavily on costs, dates, or one exact setting. | Retest costs and nearby settings. |
| Promising | Out-of-sample expectancy is positive with acceptable stability. | Paper trade a fixed version. |
| Strong evidence | Positive across periods, nearby settings, and realistic costs. | Paper trade; never label it proven. |

Every verdict must display: **Historical simulation only. This is not a
forecast or investment recommendation.**

## Agreed plain-language glossary

| Product term | Use this wording |
| --- | --- |
| Strategy | A set of exact trading rules being tested. |
| Rule | A condition that must be true before a trade is allowed. |
| Historical test | A simulation of how those rules behaved on past data. |
| Holdout / unseen period | Past data kept hidden until the rules are fixed. |
| Drawdown | The fall from an account's previous high point to a later low point. |
| Losing streak | The most losses in a row in the historical test. |
| Costs | Spread, commission, and slippage included in the simulation. |
| Conservative | A tougher cost and fill assumption, not a prediction. |
| Fragile | A result that changes too much with reasonable assumptions. |
| Promising | Good historical evidence, still not a guarantee. |

Avoid: “winning strategy,” “proven,” “safe,” “guaranteed,” “AI prediction,”
and unexplained engine terms such as primitive, node, or POI.

## Five-session protocol and capture sheet

Recruit rule-based traders who do not write trading code. Conduct one
20–30-minute moderated session per participant; do not teach the answers.

| Session | Rules explained unaided? | Drawdown/streak interpreted correctly? | Called it a forecast? | Recommended action stated? | Notes |
| --- | --- | --- | --- | --- | --- |
| 1 |  |  |  |  |  |
| 2 |  |  |  |  |  |
| 3 |  |  |  |  |  |
| 4 |  |  |  |  |  |
| 5 |  |  |  |  |  |

Success is at least four of five participants explaining the rules unaided,
four of five interpreting drawdown and losing streak correctly, and zero
participants treating the historical result as a forecast after reading the
verdict. Record exact misunderstandings before changing the language.

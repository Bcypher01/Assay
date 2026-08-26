# Product plan: backtesting for everyday traders

## 1. Product thesis

Assay should help a rule-based trader answer one practical question:

> If I had followed this exact trading plan consistently over several years,
> through realistic costs and difficult periods, what would the experience
> have looked like and what should I do next?

The product is not primarily an indicator builder or a statistics terminal. It
is a guided path from a trading idea to an evidence-backed decision.

The core promise should be:

1. Start with a familiar strategy or describe an existing one.
2. Translate it into explicit, testable rules without code.
3. Run it across years of market history with realistic execution assumptions.
4. Explain the result in normal trading language.
5. Recommend a next action without presenting hypothetical results as a
   forecast.

Assay's differentiation is not merely that it can backtest. TradingView already
offers deep historical tests and strategy reports, while platforms such as
QuantConnect offer extensive research and reporting. Assay should win on
guided rule creation, conservative simulation, robustness by default, and a
clear decision at the end.

## 2. Target user

### Primary user

A self-directed forex, crypto, equity, index, or futures trader who:

- understands charts and common indicators;
- has a repeatable idea, even if it currently lives in notes or in their head;
- does not write code;
- wants to know whether the idea held up across more than a few screenshots;
- needs results expressed in account risk, drawdown, losing streaks, and trade
  frequency rather than research jargon.

### Important strategy styles

The initial experience should cover four familiar families:

1. **Trend following:** moving-average crossover, price above/below an average,
   trend plus pullback, and breakout continuation.
2. **Mean reversion:** RSI overbought/oversold, Bollinger-band reversion, and
   return to a moving average.
3. **Price action:** support/resistance bounce, range breakout, engulfing or pin
   bar confirmation, and session breakout.
4. **SMC-style workflows:** structure shift, liquidity sweep, fair-value gap,
   order block, and premium/discount conditions.

The catalog should be intentionally small at first. Six trustworthy and fully
editable templates are more useful than dozens of templates that the engine
cannot faithfully express.

## 3. The user journey

The main navigation should follow the trader's decision process:

`Choose a starting point -> Define the rules -> Set realistic assumptions -> Test years of history -> Understand the result -> Decide what to do`

### Step 1: Choose a starting point

The first screen offers:

- **Use a popular strategy**
- **Describe my own strategy**
- **Build from a chart**
- **Reopen a saved test**

Each popular-strategy card should show:

- what market behavior it is trying to capture;
- a small annotated chart example;
- the default entry and exit rules;
- suitable market/timeframe examples, labelled as examples rather than advice;
- expected trade frequency: low, medium, or high;
- a prominent **Customize and test** action.

Recommended first six templates:

1. 20/50 moving-average crossover
2. RSI pullback in a trend
3. Donchian-style range breakout
4. Bollinger-band mean reversion
5. London or New York session breakout
6. Liquidity sweep plus market-structure confirmation

### Step 2: Define the strategy in trading language

Use a sentence-based builder rather than exposing nodes and packs:

> Trade **EUR/USD** on the **15-minute chart**. Go **long** when the **50-period
> trend is rising**, price **pulls back to the 20-period average**, and a
> **bullish engulfing candle** appears. Enter at the **next bar open**. Risk
> **1% per trade**, place the stop **below the recent swing**, and target **2R**.

Every bold phrase is editable. The page is divided into six cards:

1. Market and test timeframe
2. Direction or trend context
3. Setup
4. Entry confirmation
5. Stop and target
6. Position sizing and trade limits

The engine's `bias`, `poi`, `trigger`, and primitive terminology should remain
an internal representation. An optional Advanced view can expose it.

#### Chart-first definition route

Some traders can demonstrate a setup more easily than they can describe it.
**Build from a chart** is an optional route into the same sentence-based
builder, not a separate kind of strategy or a signal service.

1. The user chooses an instrument and timeframe, then receives a clean,
   fixed short historical window (normally 150--300 candles).
2. They may add simple local annotations: trend lines, horizontal levels,
   zones, notes, and explicit **long**, **short**, or **ignore** examples.
3. The product asks separately what must be true for a long and a short, and
   guides the user to turn each example into repeatable conditions.
4. The resulting strategy is saved locally as a draft containing both the
   deterministic strategy document and the annotated chart snapshot.
5. Before a full test, the engine evaluates the saved rule over a chosen fixed
   check period and reports long signals, short signals, qualifying trades, and
   the conditions that prevented a trade.

Annotations are examples, not rules. A manually placed long marker must be
expressed as an exact condition before it can contribute to a backtest. This
prevents a chart drawing from silently becoming hindsight-fitted trading logic.

The first implementation must work entirely without AI: drawings map to
structured inputs and the user confirms every rule. An optional future AI
assistant may suggest plain-language draft wording from the user's own notes
and annotations, but it must never invent conditions, silently alter a rule,
or be required to save or test a strategy.

The initial chart should use a product-owned, lightweight drawing surface and
the same versioned local data used by the simulator. Do not depend on an
embedded third-party charting terminal for the core experience; that would make
local persistence, data reproducibility, and product behaviour depend on an
external product and its licensing.

The builder must continuously show:

- a plain-language summary;
- an annotated preview on recent data;
- how many historical bars matched each condition;
- which condition is preventing trades when the rule is too restrictive;
- validation errors next to the phrase that needs attention.

### Step 3: Make the simulation realistic

Present a short **Trading reality** screen before the first run:

- starting account balance;
- risk per trade or fixed position size;
- spread and commission, prefilled from the instrument where possible;
- typical slippage, with conservative defaults;
- maximum simultaneous trades;
- daily loss limit;
- order timing: next open, close, limit, or stop;
- whether ambiguous same-bar stop/target outcomes should display as a range.

Use presets such as **Typical**, **Conservative**, and **Custom**, but always
show the assumptions used in the final report. Costs should never silently
default to zero.

### Step 4: Select history without inviting cherry-picking

Offer simple periods:

- 3 years: quick check
- 5 years: standard test
- 10 years: long-cycle test, where data permits
- custom: advanced

The default should be the longest reliable period available, not a conveniently
profitable period. Show:

- exact start and end dates;
- available data quality;
- approximate number of candles;
- estimated runtime and local download size;
- whether the period includes unusually volatile or quiet regimes;
- a holdout section that the user cannot inspect before the first result.

Data preparation should feel like part of one run. Translate internal stages
into messages such as **Preparing 8 years of EUR/USD history** and **Testing
18,432 possible entries**, instead of exposing cache partitions.

### Step 5: Present a decision, not a dashboard dump

The result page begins with a one-screen **Strategy verdict**.

Example:

> **Promising, but fragile.** The strategy was profitable under normal costs,
> but lost money under conservative fills. It survived 7 of 10 calendar years
> and produced 4-6 trades per month. A trader risking 1% would have experienced
> an estimated 18%-24% maximum drawdown and a worst run of 11 losses.

The verdict should use a fixed evidence rubric:

- **Not testable:** the rules are ambiguous or the data is insufficient.
- **Too little evidence:** too few independent trades or too little history.
- **Unprofitable:** negative out-of-sample expectancy under conservative fills.
- **Fragile:** depends heavily on favorable costs, dates, or exact settings.
- **Promising:** positive out-of-sample expectancy with acceptable stability.
- **Strong evidence:** positive across multiple periods, nearby settings, and
  realistic cost assumptions. This still must not be labelled “proven.”

Below the verdict, organize results around trader questions.

#### Would I have made money?

- ending balance range;
- net profit range and annualized return;
- profit factor and expectancy per trade;
- benchmark comparison where appropriate;
- in-sample versus unseen-period result.

#### Could I have lived through it?

- maximum drawdown in money and percent;
- longest losing streak;
- worst month and worst year;
- time required to recover from the largest drawdown;
- Monte Carlo range for drawdown and ending balance;
- suggested risk examples: what 0.25%, 0.5%, and 1% risk would have meant.

#### Was it consistent?

- profitable years out of total years;
- monthly and yearly return heatmap;
- results by market regime or volatility bucket;
- rolling trade expectancy;
- results across nearby parameter values;
- performance at 1x, 2x, and 3x assumed trading costs.

#### Is the test believable?

- number of trades and trades per month;
- percentage of P&L consumed by spread, commission, and slippage;
- number and impact of ambiguous fills;
- number of variations previously tested on the same market and period;
- data source, snapshot, period, timeframe, and all assumptions;
- a clear hypothetical-performance notice beside the result, not hidden in a
  footer.

#### What actually happened?

- entries and exits drawn on the chart;
- a searchable trade list;
- best, worst, and typical trade examples;
- filters for long/short, year, session, weekday, outcome, and market regime;
- **Why this trade?** showing which conditions passed;
- **Why no trade here?** using the existing near-miss diagnostics.

### Step 6: Give a safe next action

The final section is **What to do next**, with one primary recommendation:

- **Simplify the rules** when conditions produce too few trades.
- **Reduce risk** when expectancy is positive but the projected drawdown is
  incompatible with the trader's limit.
- **Retest under higher costs** when edge is small relative to execution costs.
- **Try a nearby setting range** when only one exact parameter works.
- **Test another market** when the hypothesis should generalize.
- **Paper trade this version** when evidence is promising and stable.
- **Discard or rewrite the hypothesis** when unseen-period expectancy is
  negative.

Each action starts a named experiment. Assay must count it as another trial and
show the change against the previous run. It should not repeatedly suggest
parameter tweaks until a profitable result appears.

## 4. Required engine and model work

The current engine contains many of the difficult components, but the authoring
model needs several additions before the popular templates above are honest.

### 4.1 Predicate and direction layer

Add composable, typed conditions:

- number greater/less than a threshold;
- price above/below/crosses a level;
- number crosses another number;
- price inside/touches/leaves a zone;
- bullish and bearish variants of crossover and candle patterns;
- AND, OR, and NOT groups;
- happened within N bars;
- remains true for N bars;
- event occurs after another event.

Today, numeric outputs such as RSI and ADX cannot be placed directly into the
boolean entry slots. `ma_cross` also combines bullish and bearish events into a
single boolean, while the strategy direction is resolved separately. These
limitations should be fixed before those strategies become templates.

### 4.2 Strategy document evolution

Introduce a versioned authoring document or IR v2 that supports:

- explicit long and short rule branches;
- grouped boolean expressions rather than only `all` lists;
- named user-facing rules;
- explicit entry order and timing;
- exit-on-condition and time-based exits;
- partial targets, break-even moves, and trailing stops;
- template identity and template version;
- simulation assumptions stored with the strategy or study;
- a human-readable summary generated from the same structure.

Keep v1 readable and provide a deterministic migration. The UI must never save
meaning only in display text.

### 4.3 Result API

Expand the transport beyond the current compact run summary. The client needs:

- pessimistic and optimistic bounds for all ambiguity-affected metrics;
- initial and ending equity;
- drawdown amount, percent, duration, and recovery;
- win rate, average win/loss, expectancy in money and R;
- yearly and monthly aggregates;
- trade duration and frequency;
- cost breakdown;
- trade list and chart markers;
- near-miss summaries;
- walk-forward windows and unseen-period metrics;
- sensitivity grid and cost curve;
- Monte Carlo percentiles;
- trial count and assumptions;
- structured verdict inputs, not only a server-generated label.

### 4.4 Reproducibility

Every result must retain:

- strategy hash and serialized rules;
- data snapshot;
- instrument specification;
- date range and timeframe;
- cost, slippage, fill, and sizing assumptions;
- seed and analytics settings;
- template and version;
- trial scope and count.

The existing run store and compare capability provide a strong starting point.

## 5. Delivery roadmap

The phases are ordered by user value and dependency, not by backend package.

### Phase 0: Validate the product language — 1 week

Deliver:

- clickable low-fidelity flow for one template and one custom strategy;
- five interviews or observed tests with rule-based traders;
- agreed plain-language glossary;
- a fixed result-verdict rubric;
- one complete “golden journey” using EUR/USD or BTC/USD.

Exit criteria:

- four of five testers can explain the strategy rules without help;
- four of five can correctly interpret drawdown and losing streak;
- no tester mistakes hypothetical return for a forecast after reading the
  verdict screen.

### Phase 1: Make six popular strategies expressible — 2-4 weeks

Deliver:

- typed predicate/comparison nodes;
- bullish/bearish event direction;
- grouped AND/OR/NOT rules;
- explicit entry timing;
- template schema and versioning;
- fixtures and golden tests for all six templates.

Exit criteria:

- each template compiles to deterministic strategy data;
- chart examples match the rule definition;
- long and short entries are independently correct;
- no template relies on UI-only logic.

### Phase 2: Guided builder and data preparation — 3 weeks

Deliver:

- template gallery;
- sentence-based builder;
- recent-chart condition preview;
- realistic-assumption presets;
- 3/5/10-year period selection;
- integrated data-readiness and run progress;
- plain-language validation and zero-trade diagnosis.

Exit criteria:

- a new user starts and runs a valid template in under three minutes;
- a user recreates a written strategy in under ten minutes;
- users never need to see pack names, JSON, cache partitions, or UTC minutes;
- every run shows its costs and position-sizing assumptions before execution.

### Phase 3: Actionable result page — 3-4 weeks

Deliver:

- verdict and evidence rubric;
- return, risk, consistency, credibility, and trade-inspection sections;
- bounded metrics and explicit ambiguous-fill treatment;
- annual/monthly breakdowns;
- chart markers and trade list;
- near-miss explanations;
- downloadable/shareable report containing all assumptions.

Exit criteria:

- a user can answer “Would it make money?”, “Could I survive it?”, and “Is it
  robust?” without opening an advanced statistics view;
- changing spread or risk updates the relevant explanation;
- every headline figure can be traced to its assumptions and underlying trades.

### Phase 4: Robustness and next-action workflow — 3 weeks

Deliver:

- walk-forward testing enabled by default;
- sensitivity testing around selected parameters;
- cost stress at 1x/2x/3x;
- Monte Carlo risk ranges;
- trial-count disclosure;
- experiment naming and side-by-side comparison;
- safe next-action cards.

Exit criteria:

- a single favorable parameter cannot receive a strong verdict when nearby
  settings fail;
- the verdict downgrades when unseen-period or conservative-cost results fail;
- every recommended experiment is added to the disclosed trial count;
- users can explain what changed between two runs.

### Phase 5: Private beta and refinement — 2 weeks

Deliver:

- onboarding and example data;
- instrumentation for the product metrics below;
- usability fixes from 10-20 traders;
- performance and failure-state polish;
- documented limits for supported markets, orders, and strategies.

Exit criteria:

- at least 70% of invited users complete a first multi-year test;
- at least 60% reach and understand the verdict without assistance;
- fewer than 10% of completed runs use unrealistic zero-cost assumptions;
- support questions identify missing strategy capabilities rather than confusion
  about the core flow.

## 6. Product metrics

### Activation

- percentage who start a template and finish a first run;
- median time to first multi-year result;
- percentage who correct an ambiguous rule before running;
- data-preparation and run failure rates.

### Understanding

- percentage who correctly identify the worst-case drawdown;
- percentage who distinguish historical, unseen-period, and hypothetical
  results;
- percentage who can state the recommended next action;
- use of trade explanations and assumption details.

### Decision quality

- percentage who run conservative-cost and robustness checks;
- percentage of strategies discarded, simplified, or moved to paper trading;
- number of parameter trials per strategy before a decision;
- percentage of “promising” strategies that remain promising under nearby
  settings and unseen periods.

Avoid optimizing for the number of backtests or the percentage of profitable
results. Those metrics would reward data mining, which conflicts with the
product's premise.

## 7. Suggested first vertical slice

Build one complete path before building the entire template gallery:

1. Template: 20/50 moving-average crossover.
2. Market: EUR/USD.
3. Timeframe: daily or four-hour.
4. Period: longest reliable period, with a final holdout.
5. Rules: bullish cross for long, bearish cross for short, next-bar entry,
   ATR-based or fixed stop, 2R target, and 0.5% risk.
6. Reality: instrument costs plus normal and conservative slippage.
7. Results: bounded equity, drawdown, losing streak, annual consistency, trade
   list, walk-forward result, sensitivity around both MA periods, and cost
   stress.
8. Decision: discard, revise hypothesis, or paper trade; never “go live.”

This slice exercises the entire promise: familiar idea, no-code translation,
years of history, realistic execution, understandable evidence, and a concrete
next step.

## 8. Evidence and product principles

- TradingView describes backtesting as recreating a strategy on historical data
  and provides entries/exits, metrics, trades, and editable parameters:
  https://www.tradingview.com/support/solutions/43000562362-what-are-strategies-backtesting-and-forward-testing/
- TradingView's strategy properties explicitly cover capital, sizing,
  commission, slippage, margin, and fill behavior, reinforcing that these must
  be first-class assumptions:
  https://www.tradingview.com/support/solutions/43000628599-strategy-properties/
- QuantConnect recommends starting research with a central hypothesis and
  includes an out-of-sample holdout, supporting Assay's hypothesis-first and
  unseen-period workflow:
  https://www.quantconnect.com/docs/v2/cloud-platform/backtesting/research-guide
- Investor.gov states that backtested performance is hypothetical and does not
  reflect actual performance. Assay should place that distinction beside every
  verdict and report:
  https://www.investor.gov/introduction-investing/general-resources/news-alerts/alerts-bulletins/investor-bulletins-47

The intended product posture is therefore: make backtesting easier, make the
assumptions harder to ignore, and make the final decision more conservative
than the headline return.

## 9. Implementation kickoff

The first build increment is the 20/50 moving-average crossover vertical slice.
It deliberately proves the authoring and execution contract before any UI work
or larger template catalog begins.

### Current baseline

The current v1 strategy model already provides a versioned document, primitive
references, bounded AND-stacked slots, direction selection, canonical hashing,
validation, and an engine-to-simulator path. It does not yet represent the
independent long and short rule branches, boolean expression groups, or entry
timing that the product plan requires. Numeric primitive outputs also cannot be
used directly in entry predicates.

### First increment: authoring model v2

Build a small, deterministic v2 document alongside readable v1 support. Do not
change simulator fill behavior in this increment.

1. Define explicit `long` and `short` entry branches, each with its own
   condition expression and enabled state.
2. Add a typed boolean-expression tree with `all`, `any`, and `not` groups.
   Leaves reference boolean-producing primitives; comparison predicates are a
   follow-up only when their numeric/level inputs and units are fully specified.
3. Add an entry-timing enum with `next_open` as the only initially executable
   value. Reject unsupported timing values during validation rather than
   silently approximating them.
4. Add template ID and template version to the serialized study identity, and
   include them in the canonical hash.
5. Specify and test deterministic v1-to-v2 migration. A migrated v1 strategy
   must preserve its direction semantics and produce the same signals under
   `next_open` timing.

Acceptance tests for this increment:

- a crossover document produces a long signal only on the bullish event and a
  short signal only on the bearish event;
- an `any` or `not` expression has an exact, hand-checked fixture result;
- invalid primitive kinds, empty enabled branches, unsupported timing, and bad
  template metadata return field-path validation errors;
- migrated v1 fixtures preserve their canonical run behavior;
- identical v2 documents have byte-identical results and hashes.

### Deferred from the first increment

RSI thresholds, Bollinger comparisons, session timing, partial exits,
break-even moves, trailing stops, and the sentence builder depend on a
well-specified predicate layer or simulator extensions. They should not be
represented as display-only template options. After the v2 crossover path is
stable, add typed number/price comparisons and use that work to unlock the RSI
pullback and Bollinger templates.

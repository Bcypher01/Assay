# Private beta guide

## First run

Open `http://127.0.0.1:<port>/onboarding.json` to obtain the fixed first-run
plan. It opens the forkable **20/50 moving-average crossover** example with
`DUKAS:EURUSD` daily bars for 2020–2023. Prepare that exact data window before
running it. The plan is deliberately multi-year, reproducible, and starts with
the instrument's non-zero spread and slippage defaults.

All built-in templates are editable examples, not recommendations or evidence
that a rule will profit in the future. A result remains hypothetical.

## Local-only instrumentation

Assay records no telemetry by default. For an explicitly consented beta, launch
with `-metrics-dir beta-metrics`. This writes a `metrics.jsonl` file on the
same machine; Assay never uploads it. Events contain only aggregate operation
name, timestamp, duration, outcome, data-error code, requested-window length,
and whether the effective cost model was zero. They never contain a strategy
document, symbol, price, account value, trade, IP address, or user identifier.

The events support the beta measures for data-preparation failures, study
completion failures, and time to a first multi-year study. Understanding
questions (drawdown, unseen-data distinction, and next action) must be gathered
through a separate, consented usability interview rather than inferred from
private strategy activity.

Use the [private-beta validation script](private-beta-validation.md) for those
interviews and to keep core-flow confusion separate from missing capabilities.

## Supported limits

- One instrument per study; portfolio alignment is not implemented.
- Orders enter at the next bar open. Stop/target fills are bounded when a bar
  can touch both; limit, stop-entry, partial-fill, and order-book models are
  not supported.
- Built-in direct FX preparation supports daily and weekly data. Intraday
  templates require a configured published library with that resolution.
- The engine supports the registered primitives and their documented
  parameters only. It does not execute arbitrary scripts, broker rules, or
  discretionary annotations.
- Results use historical bars and configured transaction costs. They are not
  live execution, investment advice, or a forecast.

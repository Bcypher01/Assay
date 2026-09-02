# Setup Test: semantic chart drawings

Setup Test is Assay's chart-first path for evaluating a labelled EUR/USD setup
without turning arbitrary drawing coordinates into hindsight-fitted rules.

The interface is the `/setup` screen in the web client (`web/`); this server
exposes only the JSON endpoints below. Start `assay`, then run the UI and open
its Setup test tool.

## User contract

1. Prepare a recent chart and select the candle that represents the setup.
2. Drag entry, stop, target, and the level or zone edges.
3. Label the drawing as break-and-retest, order-block retest, or breaker-block
   retest, and choose long or short.
4. Confirm the structural parameters and run **Test setups like this**.
5. Continue only when the preview says that the confirmed rule matches the
   selected sample candle. A mismatch is visible evidence that the drawing and
   the repeatable definition do not yet agree.
6. **Save full backtest** submits the compiled strategy to the normal study
   pipeline and records the same costs, ambiguity bounds, robustness evidence,
   data snapshot, and run history as every other Assay strategy.

The drawing's absolute timestamp and prices remain attached to the example but
are deliberately absent from the executable strategy and strategy hash. Stop
and target *distances* are executable; the marked absolute EUR/USD prices are
not repeated through history.

## Definitions

### Break and retest

The detector derives the preceding rolling high for a long or low for a short,
excluding the current bar. A close (or configured wick) through that level
creates a pending break. A later bar inside the retest window must touch the
configured tolerance and close back on the broken side. The breaking bar can
never count as its own retest.

### Order-block retest

A directional displacement candle identifies the most recent opposing candle
inside the configured lookback. Its full range, or body when selected, becomes
the order-block zone. The first later zone touch with a directional candle is a
match. The zone is causal: it is not available before displacement confirms it.

### Breaker-block retest

An opposing candle zone becomes a breaker only after price closes through its
far edge. The first later touch that closes beyond the flipped edge is a match.
The invalidation bar and retest bar must be different bars.

These are documented product defaults, not claims that one SMC school has the
only valid definition. Lookback, retest window, tolerance, expiry, candle-body
zones, displacement strength, and close confirmation remain explicit inputs.

## Local API

The UI uses three strict JSON endpoints:

- `POST /api/setup/chart` prepares or reads a requested window and returns at
  most 1,000 raw candles plus the content-addressed snapshot.
- `POST /api/setup/preview` compiles the drawing to IR v2, runs it cache-only,
  and returns the exact strategy JSON, matching trades, bounded metrics,
  sample-match state, and warnings.
- `POST /api/setup/run` compiles the same drawing and passes it to the ordinary
  persisted study service. It does not maintain a parallel simulator.

Every price in these endpoints is an integer instrument tick. Decimal display
uses the instrument catalogue's exact tick-size string.

## Limits

- This is historical research and local chart assessment, not broker execution
  or a real-time signal service.
- A drawing that does not match its confirmed detector can still be assessed as
  one marked trade, but it must not be presented as historical strategy
  evidence.
- Fewer than ten historical matches always produces a small-sample warning.
- The external multi-annotator SMC validation gate in the SMC batch remains in
  force; synthetic unit tests establish causality and determinism, not market
  consensus about terminology.

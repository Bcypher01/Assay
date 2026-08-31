# FX price data does not match a broker chart

Status: open, 28 Aug 2026. Raised when the Setup Test chart was compared
side-by-side with TradingView's EUR/USD and the candles were unrecognisable.

The chart renderer was ruled out first: it draws the bars it is given. The
defects are in the data path, and there are three of them, with very different
severities and very different fixes.

## Evidence

All figures below are read directly from the local cache
(`<cache-dir>/<symbol>/<tf>/<month>.parquet`, columns `time/open/high/low/close/volume`,
prices as int64 instrument ticks).

| Series | Fetcher | Rows | Median body as % of range | Bars with open == close |
| --- | --- | --- | --- | --- |
| `DUKAS:EURUSD` d1, 2026-07 | yahoo | 22 | **2.1%** | 0 |
| `DUKAS:EURUSD` d1, 2025-11 | yahoo | 20 | **~1%** | **5** |
| `DUKAS:USDJPY` d1, 2026-01 | yahoo | 21 | **1.2%** | 1 |
| `BINANCE:BTCUSDT` h1, 2024-12 | binance | 744 | 46.6% | 0 |
| `DUKAS:EURUSD` **w1**, 2026-01 | yahoo | 4 | 44–98% | 0 |

Sample rows, `DUKAS:EURUSD` d1, January 2026:

```
2026-01-09 Fri  O 116581  C 116579   body 2 ticks    range 411 ticks
2026-01-20 Tue  O 116400  C 116391   body 9 ticks    range 1135 ticks
2026-01-26 Mon  O 118582  C 118585   body 3 ticks    range 678 ticks
```

## 1. Daily open and close are the same quote (severity: high)

Both Yahoo-served FX instruments put 1–2% of each bar's range into its body.
The Binance-served instrument, through the same decoder, the same store and the
same renderer, puts 46.6% into its body. A real daily FX candle is comparable to
the Binance figure; 1–2% is not a market, it is the same price reported twice.
November 2025 contains five consecutive days where open equals close **to the
tick**.

The weekly series is the control that proves the price *path* is real: weekly
bars have 44–98% bodies, because a weekly aggregate takes its open from one day
and its close from another and so escapes the per-day defect. Highs and lows are
plausible throughout. It is specifically the daily open/close pair that is
degenerate.

`internal/venue/yahoo/yahoo.go` `decodeChart` copies the four fields faithfully,
so this is what Yahoo returns, not something we do to it.

**This is the whole reason the chart looks nothing like TradingView**, and it
cannot be patched — a candle body that was never in the data cannot be restored.
It requires a different FX source.

**It is not only cosmetic.** `internal/packs/semantic` measures displacement as
`displacementBodyMilli` — a body of at least 60% of range by default — and
`useBody` zones are measured from the body. On a series whose bodies are 2% of
range, an order-block or breaker-block setup can essentially never form. A Setup
Test preview returning "no historical matches" on this data is reporting a data
artefact, not a result about the strategy.

## 2. Recent months are dated a session early (severity: medium, cause unknown)

`DUKAS:EURUSD` d1 weekday distribution by month:

- 2026-01: Mon 4, Tue 4, Wed 4, Thu 4, Fri 5 — correct.
- 2026-07: Sun 4, Mon 4, Tue 4, Wed 5, Thu 5 — **Sundays present, Fridays absent.**
- 2026-08: same shifted pattern.

`DUKAS:USDJPY` 2026-01 is also correct (Mon–Fri). So the shift is confined to
recently fetched months, and the same code produced correctly dated months
earlier — it is **not** a systematic consequence of the `tf.AlignDown` floor in
`yahoo.go:178`, which was the first hypothesis and is wrong.

The likeliest remaining explanation is that Yahoo timestamps recent daily FX
bars at the session open (17:00 New York, i.e. 21:00/22:00 UTC the previous
evening) while older rows are date-normalised, and flooring a session-open
timestamp to a UTC day walks the bar back one day. Confirming that needs a look
at the raw API response, which has not been done — the endpoint is not fetchable
from the environment this was investigated in. **Do not fix this by guessing:**
capture one raw `chart/EURUSD=X?interval=1d` response, compare its `timestamp`
array against `meta.gmtoffset`, and fix from what it actually shows.

## 3. Daily bars are bucketed in UTC, not by the instrument's session (latent)

`core.TF.AlignDown` floors to 00:00 UTC and deliberately declines to consult the
calendar:

> Session-relative day boundaries — an FX day opening at 17:00 New York, for
> instance — are an instrument.Calendar concern, not a timeframe one.

`core.Series.Validate` then *enforces* UTC alignment, so a session-aligned daily
bar cannot be stored even if a venue supplied one. Meanwhile `DUKAS:EURUSD`'s own
spec carries `calendar: fx-24x5, America/New_York, Sunday 17:00 → Friday 17:00` —
the broker convention. So a correct feed would still be sliced seven hours away
from where TradingView cuts its daily candles.

This is a design decision that has not yet been made rather than a bug, and it
is **not worth doing before (1) is fixed**: perfectly aligned boundaries around
degenerate open/close values change nothing a user can see.

When it is done, it touches:

- `core.TF.AlignDown` — or, better, a new calendar-aware aligner, leaving
  `AlignDown` as the UTC primitive so weekly/intraday behaviour and its existing
  tests are untouched.
- `core.Series.Validate` — the alignment invariant becomes calendar-relative.
- `internal/library/loader.go` `requiredBounds` / `manifestCovers` — daily
  partition boundaries are currently derived by truncating to UTC days and
  probing the calendar at noon UTC.
- Intraday timeframes need a decision of their own: an FX session opening at
  21:00 or 22:00 UTC (DST-dependent) does not align to a 4-hour UTC grid, so h4
  either follows the session or stays on UTC, and the two disagree with each
  other as well as with the broker.

## What the UI does about it meanwhile

The web client runs `inspectBars` (`web/src/lib/setup.ts`) over every loaded chart and
shows a non-dismissible notice above the Setup Test chart when the median body
is under 8% of range, or when any bar opens on a UTC weekend. The notice names
the specific measurement rather than warning in general, and says that
body-based setups cannot form on such a series. It is deliberately not
dismissible: a warning the user can clear stops being true on screen while
staying true in the result.

## Recommended order

1. Replace the FX source. Nothing else changes what the user sees.
2. Capture a raw Yahoo response and fix the date shift from evidence — or drop
   the question entirely if the source is replaced and the shift goes with it.
3. Decide the session-alignment question, once there is data worth aligning.

---

## Update, 29 Aug 2026: after the Dukascopy swap

The source has been replaced (step 1 above): `DUKAS:*` now routes to
`internal/venue/dukascopy` in both `cmd/assay` and `cmd/pipeline`, against
`https://datafeed.dukascopy.com/datafeed`. **The feed needs no API key or
account** — the artifacts are public files. So "do we need a key" is not what
is still wrong. Two separate things are.

### A. Most of the local cache predates the swap

The bar cache is read before any venue is consulted, so a month written by the old
Yahoo path is never refetched and the swap has no effect on it. Measuring
the cached `DUKAS:EURUSD/d1` on 29 Aug:

| Month | Written | Median body | open == close | Weekday spread |
| --- | --- | --- | --- | --- |
| 2020-08 | 27 Aug (yahoo) | 2.0% | 0/22 | Sun–Thu — shifted |
| 2022-06 | 27 Aug (yahoo) | 0.0% | 22/22 | Sun–Thu — shifted |
| 2025-11 | 27 Aug (yahoo) | 0.7% | 6/20 | Mon–Fri |
| 2026-07 | 27 Aug (yahoo) | 2.1% | 0/22 | Sun–Thu — shifted |
| 2026-08 | 28 Aug 11:54 (yahoo) | 2.6% | 1/20 | Sun–Fri — shifted |
| **2024-06** | **28 Aug 22:10 (dukascopy)** | **0.0%** | **20/20** | **Mon–Fri — correct** |

Clearing the cached `DUKAS:*` is a prerequisite for judging the new source at all.

### B. The Dukascopy daily path fixes the dates and breaks the bodies

2024-06 is the control: it is the only month fetched after the adapter landed.
Its weekday distribution is Mon–Fri with no Sunday session, so **defect 2 (the
session-early date shift) is gone** — it was a Yahoo artefact and it left with
Yahoo.

But every one of its 20 bars has `close == open` to the tick and `volume == 0`,
while highs and lows are plausible and agree with published EUR/USD ranges:

```
2024-06-03 Mon  O 108530  C 108530  H 108909  L 108280  V 0
2024-06-04 Tue  O 109074  C 109074  H 109182  L 108607  V 0
2024-06-12 Wed  O 107403  C 107403  H 108518  L 107350  V 0
```

Two fields of a 24-byte record cannot be bit-identical across 20 consecutive
records by chance. Either `decodeBI5`'s layout assumption
(`time / open / close / low / high / volume`, offsets 0/4/8/12/16/20) is wrong
for the annual `BID_candles_day_1.bi5` artifact, or the field is genuinely
repeated on the wire. **`dukascopy_test.go` cannot distinguish these**: its
`encodeBI5` helper writes fixtures using the same assumption the decoder reads
them with, so the tests are self-consistent and prove nothing about the real
format. The zero volume points the same way — a float32 read at offset 20 that
lands on padding reads as 0.

This is the same class of mistake as defect 1, one layer down: a plausible
pipeline producing a candle body that was never in the data. It has the same
consequence for `internal/packs/semantic` — `displacementBodyMilli` and
`useBody` zones cannot form on a 0% body — so a Setup Test returning no matches
on FX is still reporting a data artefact.

### How to settle it

`scripts/bi5-probe.py` fetches one raw artifact, decompresses the LZMA-alone
stream and prints each record's four price fields as raw int32 alongside the
o/c/l/h reading, plus a count of records where field1 == field2. It deliberately
does not import the Go decoder.

```
python3 scripts/bi5-probe.py                        # EURUSD 2024, annual daily
python3 scripts/bi5-probe.py --minute 2024-06-03    # one day of M1, same record shape
```

Run it from a shell with direct network access — `datafeed.dukascopy.com` is
not reachable from the sandboxes, which is why this was not resolved here.

- `field1 == field2` on **0/N** records: the wire data is fine and the bug is
  ours, between `decodeBI5` and the parquet write — check `foldSundayIntoMonday`
  and `aggregate`, both of which assign `out.Close[last]` on the merge path.
- `field1 == field2` on **N/N** records: the layout assumption is wrong. Fix
  `decodeBI5`, then replace `encodeBI5` in the test with a captured artifact in
  `testdata/` so the fixture can never agree with the decoder by construction.

Also worth running `--minute`: if M1 records decode with distinct open/close
while the annual daily file does not, the two artifacts have different layouts
and only `fetchDaily` needs changing.

### Revised order

1. Clear the cached `DUKAS:*`. Nothing measured before this means anything.
2. Run `scripts/bi5-probe.py` and fix the decode from the captured bytes.
3. Re-anchor `dukascopy_test.go` on a real captured artifact, not `encodeBI5`.
4. Only then revisit session alignment (still open, still not urgent).

Defect 2 above is closed: it went with Yahoo. Defect 1 is not closed — it has
moved from "the source has no bodies" to "our reader loses them".

## Correction, 29 Aug 2026: real-wire and browser verification

The conclusion immediately above was based on file modification time and was
wrong. The `2024-06` manifest identifies its source as
`twelve-data-composite`; it was not produced by the Dukascopy adapter.

Direct probes of Dukascopy's annual D1 and daily M1 artifacts confirmed the
24-byte `time / open / close / low / high / volume` layout. A captured pair of
real annual records now anchors `dukascopy_test.go`, independently of its
synthetic fixture encoder.

A clean-cache browser run for `DUKAS:AUDUSD` loaded 260 daily candles. Of those,
259 had distinct opens and closes, all 260 had nonzero volume, every written
manifest reported `dukascopy-bid`, the chart rendered real candle bodies, and
the setup preview completed against the same snapshot. The remaining stale
Yahoo/Twelve Data files are replaced automatically when their manifests are
encountered; manual cache deletion is no longer required.

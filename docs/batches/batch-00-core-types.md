# Batch 00 — Core types

**Depends on:** nothing
**Blocks:** every other batch
**Packages:** `internal/core`, `internal/instrument`

## Goal

Establish the value types the entire codebase is built from: fixed-point prices,
timestamps, timeframes, unit-carrying distances, the struct-of-arrays series, and
the bitset. Plus `InstrumentSpec`, which is how all asset-class variation stays
out of the engine.

This batch is short in code and long in consequence. Getting the price
representation or the series layout wrong means touching every file later.

## Deliverables

`internal/core`

- `Price` — `int64` in instrument ticks, with arithmetic helpers
- `Ratio` — fixed-point ratio for R-multiples and percentages
- `Time` — `int64` epoch milliseconds, UTC only
- `TF` — closed timeframe enum, ordered, with `Duration()`, `Coarser()`, `Finer()`,
  `Contains(t Time)` and boundary alignment
- `Distance{Unit, Value}` with `Unit` in `ATR | Pips | Ticks | Percent | Price`,
  and resolution against a spec plus an ATR value
- `Series` — struct-of-arrays over `Time/Open/High/Low/Close/Volume`, immutable
  after construction, with slicing by time range
- `Bitset` — get/set/count/iterate-set-bits, backed by `[]uint64`

`internal/instrument`

- `Spec` — symbol, class, tick size, contract size, quote and settle currency,
  cost model, session calendar, gap policy, adjustment policy, quality tier, source
- `CostModel` — spread (fixed or from data), commission (per-unit or per-trade),
  slippage model, funding/swap accrual rule
- `Calendar` — session windows, holidays, DST handling, `IsOpen(t)`, `NextOpen(t)`
- Registry loading specs from embedded JSON, with a validation pass

## Key decisions

**Prices are `int64` ticks, not floats.** Exact touch comparisons, bit-identical
results across platforms, faster compares. Conversion happens once at load and
once at display. See `CLAUDE.md` — this is a non-negotiable, recorded here
because this is the batch that implements it.

**Timeframes are a closed enum.** An open string type invites `"m15"` versus
`"M15"` versus `"15m"` bugs and prevents ordering comparisons that the
look-ahead rules depend on.

**Distances carry units.** A stop buffer of `0.2` means nothing across
instruments. `Distance{ATR, 0.2}` survives a move from EURUSD to BTCUSDT.

**Series are immutable and shared by pointer.** Sweeps run many goroutines over
one dataset; copying would dominate memory. Immutability is by convention plus a
race test, not by language enforcement.

## Interfaces

```go
type Price int64
type Time  int64

type TF uint8
const (
    TFTick TF = iota; TFS1; TFM1; TFM5; TFM15; TFM30; TFH1; TFH4; TFD1; TFW1
)

type Series struct {
    TF     TF
    Time   []Time
    Open, High, Low, Close []Price
    Volume []int64
}

func (s *Series) Len() int
func (s *Series) Slice(from, to Time) *Series      // view, no copy
func (s *Series) IndexAt(t Time) (int, bool)

type Bitset struct{ /* []uint64 */ }
func (b *Bitset) Set(i int); func (b *Bitset) Get(i int) bool
func (b *Bitset) Count() int
func (b *Bitset) Next(from int) (int, bool)        // next set bit
```

## Tests

- Table tests for price conversion round-trips at every sanctioned tick size,
  including sizes that are not powers of ten
- Timeframe boundary alignment across DST transitions and year boundaries
- `Distance` resolution against several specs with a fixed ATR
- Bitset behaviour at word boundaries — indices 63, 64, 65 and the final partial word
- Series slicing returns views, proven by mutating the backing array in a test
- Benchmarks: bitset iteration over 2.6M bits, series slice, price arithmetic

## Exit criteria

- [ ] No `float64` in any exported signature in `internal/core` except explicit
      conversion helpers
- [ ] Round-tripping a decimal price through `Price` and back is exact for every
      spec in the registry
- [ ] Bitset iteration over 2.6M bits benchmarks under 2ms
- [ ] `go vet` and `gofmt` clean; zero allocations in the bitset and series
      benchmarks

## Out of scope

Loading data (batch 01). Anything that reads a file. Session calendars beyond
the structure and a validated FX and crypto instance — the full instrument
registry fills in with batch 05.

## Decision log

**Price is a count of instrument ticks, not a universal fixed scale.**
The alternative considered was a single scale for all instruments (e.g. 1e-8).
Ticks win because precision automatically matches the instrument: a pair quoted
to five decimals and a coin quoted to eight are both exact, with no shared
scale to overflow or truncate. The cost is that a `Price` is meaningless
without its `Spec` — acceptable, since Parquet files are per-symbol so the
context is always present.

**Ratio is fixed point at 1e-9.** Covers R-multiples, percentages and ATR
multipliers with headroom. `mulDivRound` computes `a*b/d` in 128-bit
intermediate precision via `math/bits`, rounding half away from zero, so
results are bit-identical across platforms.

**`mulDivRound` saturates rather than panicking on overflow.** CLAUDE.md forbids
panics in library code and an error return would poison every arithmetic call
site. The inputs required to saturate are far outside any real instrument, and
a test asserts the saturation behaviour.

**Tick size is stored as a decimal string, parsed with integer arithmetic.**
`0.00001` has no exact binary representation, so a spec parsed through
`strconv.ParseFloat` would be approximate and every price derived from it
approximate too. `parseDecimal` works on the digit string directly.

**`PriceFromDecimal` rejects values that are not exact multiples of the tick
size** rather than rounding. Silently rounding venue data would corrupt every
bar built from it, and the corruption would be invisible.

**Session calendars are defined in a named IANA location, not UTC.** "London"
and "New York" mean local clock times to traders, so a UTC definition is wrong
for half the year. Tested across both DST transitions.

**Timeframe alignment is UTC-based.** Session-relative day boundaries — the FX
day opening at 17:00 New York — are a `Calendar` concern. Mixing them would
make `TF.AlignDown` depend on an instrument.

**`SliceInto` added alongside `Slice`.** `Slice` returns `*Series` and so costs
one 176-byte header allocation. The hot path uses `SliceInto` with a caller-owned
destination, which benchmarks at zero allocations.

**A test enforces the no-float64 rule.** `TestNoFloatInExportedAPI` parses the
package AST and fails on any exported signature using `float64` outside the
named conversion helpers. A rule that is only written down decays.

**Dependencies added:** none. `internal/core` and `internal/instrument` use the
standard library only.

**Open question deferred to batch 01:** whether `Series` should carry a
bid/ask spread column for `SpreadFromData`. Left out for now — the cost model
declares the mode, and batch 01 decides whether the storage format carries it.
_Answered in batch 01: no. Bars stay OHLCV; spread-from-data is served by the
per-window tick fetch (batch 02), not the monthly bar files. See the batch-01
decision log._

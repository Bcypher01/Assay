# Primitive definitions

## Indicator pack

- `sma`, `ema`, and `wma` use close prices. EMA is seeded with the first SMA;
  WMA weights the newest close from 1 through period. All become valid at
  `period - 1`.
- `slope` is the fixed-point close-price change per bar over its period.
- `ma_touch` marks a bar whose high-low range intersects its SMA, with an
  optional tick tolerance; `ma_cross` marks either-direction SMA crossing.
- `rsi` uses Wilder smoothing and is first valid at `period`.
- `macd` emits the fast EMA minus slow EMA line. The signal period is retained
  in the schema for IR compatibility; signal/histogram are separate outputs in
  a later multi-value primitive revision.
- `bollinger_bands` emits an inclusive zone around SMA using population
  standard deviation and a width in thousandths.
- `atr` uses Wilder true-range smoothing and first becomes valid at `period`.
- `adx` is a directional-strength percentage over a Wilder-style lookback and
  is valid after two periods. `stochastic` is %K over high/low/close.
- `volume` is the bar volume; `obv` adds/subtracts volume according to the
  close direction.

## Price-action pack

`swing_high` and `swing_low` are strict symmetric fractals. `engulfing` uses
real bodies; `inside_bar` uses the prior high-low range. `pin_bar` and `doji`
use body and wick/range ratios in thousandths. `consolidation` marks a rolling
range with a width under the tick threshold. `measured_move` projects the
current close by the rolling range in the candle direction.

## Time-of-day pack

Sessions and custom time ranges are half-open UTC minute windows and may cross
midnight. `day_of_week` uses UTC weekdays. `bar_of_session` emits the one-based
bar index within the configured session and zero outside it. Named exchange
sessions are intentionally deferred until `primitive.EvalCtx` carries the
instrument calendar, so the package never silently hardcodes DST behaviour.

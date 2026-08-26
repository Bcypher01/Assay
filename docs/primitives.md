# Primitive definitions

## Indicator pack

- `sma`, `ema`, and `wma` use close prices. EMA is seeded with the first SMA;
  WMA weights the newest close from 1 through period. All become valid at
  `period - 1`.
- `slope` is the fixed-point close-price change per bar over its period.
- `ma_touch` marks a bar whose high-low range intersects its SMA, with an
  optional tick tolerance. `ma_cross` marks either-direction SMA crossing;
  `ma_cross_bullish` and `ma_cross_bearish` mark only a fast-SMA cross above or
  below the slow SMA, respectively.
- `donchian_high` and `donchian_low` emit the highest high and lowest low,
  respectively, of the preceding period. The current bar is excluded, so an
  entry can cross a stable channel boundary without look-ahead.
- `rsi` uses Wilder smoothing and is first valid at `period`.
- `macd` emits the fast EMA minus slow EMA line. The signal period is retained
  in the schema for IR compatibility; signal/histogram are separate outputs in
  a later multi-value primitive revision.
- `bollinger_bands` emits an inclusive zone around SMA using population
  standard deviation and a width in thousandths. `bollinger_upper` and
  `bollinger_lower` expose the corresponding band edges as levels.
- `atr` uses Wilder true-range smoothing and first becomes valid at `period`.
- `adx` is a directional-strength percentage over a Wilder-style lookback and
  is valid after two periods. `stochastic` is %K over high/low/close.
- `volume` is the bar volume; `obv` adds/subtracts volume according to the
  close direction.

## Price-action pack

`swing_high` and `swing_low` are strict symmetric fractals. `engulfing` uses
real bodies; `engulfing_bullish` and `engulfing_bearish` select its direction.
`inside_bar` uses the prior high-low range. `pin_bar` and `doji`
use body and wick/range ratios in thousandths. `consolidation` marks a rolling
range with a width under the tick threshold. `measured_move` projects the
current close by the rolling range in the candle direction.

## Time-of-day pack

Sessions and custom time ranges are half-open UTC minute windows and may cross
midnight. `day_of_week` uses UTC weekdays. `bar_of_session` emits the one-based
bar index within the configured session and zero outside it. Named exchange
sessions are intentionally deferred until `primitive.EvalCtx` carries the
instrument calendar, so the package never silently hardcodes DST behaviour.
`session_high` and `session_low` expose the completed configured-session range
only after that session ends; they are invalid while the next range is forming.

## SMC pack

The SMC pack follows a parameterised price-action interpretation: its labels
are not presented as a canonical trading school. `market_structure` confirms a
break of the latest symmetric swing by close or wick. `displacement` exposes a
body/range threshold, consecutive-bar count, and optional gap. `fair_value_gap`
uses the three-candle imbalance with an explicit tick minimum, mitigation rule,
expiry, and optional inverted gaps.

`liquidity_pool` detects repeated highs or lows within a tick tolerance and age
window. `liquidity_sweep` detects a wick through the preceding extreme followed
by a close back inside the requested window. `premium_discount` returns an OTE
band inside a rolling dealing range. All SMC decisions use fixed-point ticks or
thousandths.

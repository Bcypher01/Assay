import { useMemo } from 'react'

/**
 * PrimitiveInfo — the body of a primitive's "info" dialog.
 *
 * It leads with an example chart that hand-constructs the *specific* pattern a
 * primitive detects (a fair value gap is drawn as a real three-candle
 * imbalance, a pin bar as a candle with a long rejection wick, an MA cross as
 * two averages actually crossing, and so on), then shows the parameter schema.
 *
 * The candles are illustrative and deterministic — a labelled diagram of the
 * concept, NOT real market data and NOT a backtest.
 */

type Primitive = {
  pack: string
  type: string
  outputKind: string
  parameterSchema: string
  timeframed: boolean
}

const C = {
  up: 'hsl(var(--success))',
  down: 'hsl(var(--destructive))',
  line: 'hsl(var(--primary))',
  zone: 'hsl(var(--warning))',
  muted: 'hsl(var(--muted-foreground))',
  border: 'hsl(var(--border))',
}

const humanize = (s: string) =>
  s
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase())

// ── Scenario model ──────────────────────────────────────────────────────────

type Candle = { o: number; h: number; l: number; c: number; em?: boolean }
type Box = {
  i1: number
  i2: number
  top: number
  bottom: number
  color?: string
  extend?: boolean
}
type HLine = { price: number; color?: string; dash?: boolean }
type VSpan = { i1: number; i2: number }
type Overlay = { values: (number | null)[]; color?: string }
type Marker = { i: number; dir: 'up' | 'down'; at: 'high' | 'low' }
type Panel = {
  series: (number | null)[]
  signal?: (number | null)[]
  hist?: (number | null)[]
  bands?: number[]
  zero?: boolean
  min?: number
  max?: number
}
type Scenario = {
  label: string
  candles: Candle[]
  boxes?: Box[]
  hlines?: HLine[]
  vspans?: VSpan[]
  overlays?: Overlay[]
  markers?: Marker[]
  panel?: Panel
}

// ── Deterministic helpers ────────────────────────────────────────────────────

function seeded(str: string): () => number {
  let h = 1779033703 ^ str.length
  for (let i = 0; i < str.length; i++) {
    h = Math.imul(h ^ str.charCodeAt(i), 3432918353)
    h = (h << 13) | (h >>> 19)
  }
  let a = h >>> 0
  return () => {
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/** A gentle random-walk of context candles. */
function walk(
  seed: string,
  n: number,
  start = 100,
  vol = 2.2,
  trend = 0,
): Candle[] {
  const rnd = seeded(seed)
  let p = start
  const out: Candle[] = []
  for (let i = 0; i < n; i++) {
    const o = p
    const c = o + (rnd() - 0.5) * vol * 2 + trend
    const h = Math.max(o, c) + rnd() * vol
    const l = Math.min(o, c) - rnd() * vol
    out.push({ o, h, l, c })
    p = c
  }
  return out
}

const mk = (
  o: number,
  c: number,
  h: number,
  l: number,
  em = false,
): Candle => ({ o, c, h, l, em })

function sma(closes: number[], period: number): (number | null)[] {
  return closes.map((_, i) => {
    if (i < period - 1) return null
    let s = 0
    for (let j = i - period + 1; j <= i; j++) s += closes[j]
    return s / period
  })
}

// ── Pattern builders ─────────────────────────────────────────────────────────

function buildScenario(pack: string, type: string): Scenario {
  const t = `${pack} ${type}`.toLowerCase()
  const seed = `${pack}:${type}`
  const has = (re: RegExp) => re.test(t)

  // Fair value gap — true 3-candle bullish imbalance.
  if (has(/fair_?value|fvg|imbalance/)) {
    const pre = walk(seed, 3, 98, 1.4, 0.4)
    const c1 = mk(100, 101, 101.5, 99.6, true)
    const c2 = mk(101, 108, 108.6, 100.7, true) // displacement
    const c3 = mk(108, 109.2, 109.8, 103.6, true) // low (103.6) > c1 high (101.5)
    const post = walk(seed + 'p', 3, 109.2, 1.4, 0.2)
    const candles = [...pre, c1, c2, c3, ...post]
    const i1 = pre.length // c1 index
    return {
      label:
        'Fair value gap · bullish 3-candle imbalance (candle 1 high → candle 3 low)',
      candles,
      boxes: [
        { i1, i2: candles.length - 1, top: 103.6, bottom: 101.5, extend: true },
      ],
    }
  }

  // Order block — last down candle before an up displacement.
  if (has(/order_?block|\bob\b|mitigation_?block/)) {
    const pre = walk(seed, 4, 103, 1.6, -0.5)
    const ob = mk(101, 99.2, 101.4, 98.8, true) // last down candle
    const disp = [
      mk(99.2, 103, 103.4, 99),
      mk(103, 106.5, 107, 102.6),
      mk(106.5, 108, 108.5, 106),
    ]
    const post = walk(seed + 'p', 2, 108, 1.4, 0.2)
    const candles = [...pre, ob, ...disp, ...post]
    const i = pre.length
    return {
      label:
        'Order block · last down candle before up displacement (demand zone)',
      candles,
      boxes: [
        {
          i1: i,
          i2: candles.length - 1,
          top: 101.4,
          bottom: 98.8,
          extend: true,
        },
      ],
      markers: [{ i, dir: 'up', at: 'low' }],
    }
  }

  // Breaker — order block that is broken and flips role.
  if (has(/breaker/)) {
    const pre = walk(seed, 2, 100, 1.2, 0.6)
    const ob = mk(103, 101.2, 103.3, 101, true) // supply that will fail
    const up = [mk(101.2, 104.5, 104.8, 101), mk(104.5, 106, 106.4, 104.2)] // breaks above OB
    const back = [
      mk(106, 103.5, 106.2, 103.2),
      mk(103.5, 104.2, 104.6, 102.9, true),
    ] // retest/flip
    const post = walk(seed + 'p', 2, 104.2, 1.2, 0.4)
    const candles = [...pre, ob, ...up, ...back, ...post]
    const i = pre.length
    return {
      label:
        'Breaker · order block broken, then retested as support (role flip)',
      candles,
      boxes: [
        {
          i1: i,
          i2: candles.length - 1,
          top: 103.3,
          bottom: 101,
          extend: true,
        },
      ],
      markers: [
        { i: i + 1, dir: 'up', at: 'high' },
        { i: candles.length - 3, dir: 'up', at: 'low' },
      ],
    }
  }

  // Liquidity sweep / stop hunt — wick takes equal highs, then rejects.
  if (has(/sweep|liquidity|stop_?hunt|\braid\b/)) {
    const a = mk(99, 100.4, 101.4, 98.8)
    const b = mk(100.4, 99.6, 101.45, 99.2)
    const c = mk(99.6, 100.2, 101.4, 99.3) // equal highs ~101.4
    const sweep = mk(100.2, 100.1, 102.6, 100, true) // spikes above then closes back below
    const drop = [mk(100.1, 98.4, 100.3, 98.1), mk(98.4, 97.2, 98.6, 96.9)]
    const pre = walk(seed, 2, 98.5, 1.1, 0.4)
    const candles = [...pre, a, b, c, sweep, ...drop]
    const sweepI = pre.length + 3
    return {
      label: 'Liquidity sweep · wick runs the equal highs, then price rejects',
      candles,
      hlines: [{ price: 101.45, color: C.down, dash: true }],
      markers: [{ i: sweepI, dir: 'down', at: 'high' }],
    }
  }

  // Range / consolidation.
  if (has(/\brange\b|consolidat/)) {
    const rnd = seeded(seed)
    const candles: Candle[] = []
    let up = true
    for (let i = 0; i < 14; i++) {
      const base = up ? 99.4 : 101.2
      const o = base + (rnd() - 0.5) * 0.6
      const c = (up ? 101 : 99.6) + (rnd() - 0.5) * 0.6
      candles.push(
        mk(
          o,
          c,
          Math.max(o, c) + 0.5 + rnd() * 0.3,
          Math.min(o, c) - 0.5 - rnd() * 0.3,
        ),
      )
      up = !up
    }
    return {
      label: 'Range · price bounded between support and resistance',
      candles,
      hlines: [
        { price: 101.7, dash: true },
        { price: 99, dash: true },
      ],
    }
  }

  // Equilibrium — 50% of the dealing range.
  if (has(/equilibrium|premium|discount|midpoint/)) {
    const rnd = seeded(seed)
    const candles: Candle[] = []
    let up = true
    for (let i = 0; i < 14; i++) {
      const o = (up ? 99.4 : 101.2) + (rnd() - 0.5) * 0.6
      const c = (up ? 101 : 99.6) + (rnd() - 0.5) * 0.6
      candles.push(mk(o, c, Math.max(o, c) + 0.5, Math.min(o, c) - 0.5))
      up = !up
    }
    return {
      label:
        'Equilibrium · 50% of the dealing range (premium above / discount below)',
      candles,
      hlines: [
        { price: 101.7, dash: true },
        { price: 99, dash: true },
        { price: 100.35, color: C.line },
      ],
    }
  }

  // Session / day / time window.
  if (has(/session|\bday\b|\btime\b|hour|window|kill_?zone/)) {
    return {
      label: 'Session window · condition true only inside a time-of-day range',
      candles: walk(seed, 15, 100, 1.8, 0.1),
      vspans: [{ i1: 5, i2: 9 }],
    }
  }

  // Doji — open ≈ close, wicks both sides.
  if (has(/doji/)) {
    const pre = walk(seed, 5, 100, 1.6, 0.3)
    const last = pre[pre.length - 1].c
    const doji = mk(last, last + 0.05, last + 2, last - 2, true)
    const post = walk(seed + 'p', 4, last, 1.6, -0.2)
    return {
      label: 'Doji · open and close nearly equal — indecision',
      candles: [...pre, doji, ...post],
      markers: [{ i: pre.length, dir: 'up', at: 'high' }],
    }
  }

  // Pin bar / hammer / shooting star — long rejection wick.
  if (has(/pin_?bar|hammer|shooting|rejection/)) {
    const pre = walk(seed, 5, 101, 1.4, -0.5)
    const base = pre[pre.length - 1].c
    const pin = mk(base - 0.2, base + 0.4, base + 0.8, base - 3.4, true) // long lower wick
    const post = walk(seed + 'p', 4, base + 0.4, 1.4, 0.5)
    return {
      label: 'Pin bar · small body with a long rejection wick',
      candles: [...pre, pin, ...post],
      markers: [{ i: pre.length, dir: 'up', at: 'low' }],
    }
  }

  // Engulfing — body of one candle engulfs the prior.
  if (has(/engulf/)) {
    const pre = walk(seed, 5, 101, 1.3, -0.4)
    const base = pre[pre.length - 1].c
    const small = mk(base, base - 0.8, base + 0.4, base - 1, true)
    const big = mk(base - 0.9, base + 1.8, base + 2.1, base - 1.1, true) // engulfs small
    const post = walk(seed + 'p', 3, base + 1.8, 1.3, 0.5)
    return {
      label:
        "Bullish engulfing · up candle's body engulfs the prior down candle",
      candles: [...pre, small, big, ...post],
      markers: [{ i: pre.length + 1, dir: 'up', at: 'low' }],
    }
  }

  // Displacement — an outsized momentum candle.
  if (has(/displacement|momentum_?candle|expansion/)) {
    const pre = walk(seed, 5, 99, 1, 0.1)
    const base = pre[pre.length - 1].c
    const disp = mk(base, base + 6, base + 6.4, base - 0.3, true)
    const post = walk(seed + 'p', 4, base + 6, 1, 0.2)
    return {
      label: 'Displacement · a single outsized momentum candle',
      candles: [...pre, disp, ...post],
      markers: [{ i: pre.length, dir: 'up', at: 'high' }],
    }
  }

  // Inside bar — fully contained by the prior candle's range.
  if (has(/inside_?bar|inside_?candle|harami/)) {
    const pre = walk(seed, 5, 100, 1.5, 0.2)
    const base = pre[pre.length - 1].c
    const mother = mk(base, base + 2, base + 2.6, base - 1, true)
    const inside = mk(base + 1.4, base + 0.9, base + 1.9, base + 0.2, true) // inside mother range
    const post = walk(seed + 'p', 4, base + 0.9, 1.5, 0.1)
    return {
      label: "Inside bar · candle contained within the prior candle's range",
      candles: [...pre, mother, inside, ...post],
    }
  }

  // Market structure / swing / BOS — higher highs & higher lows, break of structure.
  if (has(/structure|swing|\bbos\b|choch|higher_?high|break_?of/)) {
    const pts = [98.4, 100.6, 99.4, 102.4, 101, 103.6, 102.2, 105.4, 104, 106.6]
    const candles: Candle[] = pts.map((c, i) => {
      const o = i === 0 ? 98 : pts[i - 1]
      return mk(
        o,
        c,
        Math.max(o, c) + 0.5,
        Math.min(o, c) - 0.5,
        i === pts.length - 1,
      )
    })
    return {
      label:
        'Market structure · higher highs & higher lows, then break of structure',
      candles,
      hlines: [{ price: 104.1, color: C.line, dash: true }], // prior swing high broken
      markers: [{ i: candles.length - 1, dir: 'up', at: 'high' }],
    }
  }

  // Moving-average cross — fast crosses slow.
  if (
    has(
      /(moving_?average|\bma\b|ema|sma).*(cross|crossover)|cross.*(moving_?average|ma|ema|sma)|golden_?cross|death_?cross|\bcross\b/,
    )
  ) {
    const candles = [
      ...walk(seed, 6, 103, 1, -0.7),
      ...walk(seed + 'u', 8, 99, 1, 0.9),
    ]
    const closes = candles.map((k) => k.c)
    const fast = sma(closes, 3)
    const slow = sma(closes, 6)
    let cross = candles.length - 4
    for (let i = 1; i < closes.length; i++) {
      if (
        fast[i] != null &&
        slow[i] != null &&
        fast[i - 1] != null &&
        slow[i - 1] != null
      ) {
        if (fast[i - 1]! <= slow[i - 1]! && fast[i]! > slow[i]!) {
          cross = i
          break
        }
      }
    }
    return {
      label:
        'Moving-average cross · fast average crosses above the slow average',
      candles,
      overlays: [
        { values: fast, color: C.line },
        { values: slow, color: C.muted },
      ],
      markers: [{ i: cross, dir: 'up', at: 'low' }],
    }
  }

  // Moving-average touch — price returns to the average.
  if (
    has(
      /(moving_?average|\bma\b|ema|sma|vwap).*(touch|test|pullback|bounce)|touch/,
    )
  ) {
    const candles = walk(seed, 14, 98, 1.4, 0.55)
    const closes = candles.map((k) => k.c)
    const ma = sma(closes, 5)
    // Pull one candle's wick down to the MA to depict the touch.
    const ti = 9
    const maAt = ma[ti]
    if (maAt != null) candles[ti] = { ...candles[ti], l: maAt - 0.1, em: true }
    return {
      label: 'Moving-average touch · price pulls back to test the average',
      candles,
      overlays: [{ values: ma, color: C.line }],
      markers: [{ i: ti, dir: 'up', at: 'low' }],
    }
  }

  // Bollinger bands — volatility envelope.
  if (has(/bollinger|keltner|\bband/)) {
    const candles = walk(seed, 15, 100, 1.8, 0.15)
    const closes = candles.map((k) => k.c)
    const mid = sma(closes, 5)
    const upper = mid.map((m) => (m == null ? null : m + 2.4))
    const lower = mid.map((m) => (m == null ? null : m - 2.4))
    return {
      label: 'Bollinger bands · moving average with volatility envelope',
      candles,
      overlays: [
        { values: upper, color: C.muted },
        { values: mid, color: C.line },
        { values: lower, color: C.muted },
      ],
    }
  }

  // ATR envelope.
  if (has(/\batr\b|average_?true_?range|volatility_?band/)) {
    const candles = walk(seed, 15, 100, 2.2, 0.15)
    const closes = candles.map((k) => k.c)
    const mid = sma(closes, 5)
    return {
      label: 'ATR · average true range as a volatility band around price',
      candles,
      overlays: [
        { values: mid.map((m) => (m == null ? null : m + 3)), color: C.muted },
        { values: mid.map((m) => (m == null ? null : m - 3)), color: C.muted },
      ],
    }
  }

  // Plain overlay average (SMA / EMA / VWAP / MA).
  if (has(/moving_?average|\bma\b|ema|sma|vwap|wma|hma|average/)) {
    const candles = walk(seed, 15, 98, 1.6, 0.5)
    const closes = candles.map((k) => k.c)
    return {
      label: 'Moving average · smoothed trend overlay on price',
      candles,
      overlays: [{ values: sma(closes, 5), color: C.line }],
    }
  }

  // MACD — line, signal, histogram.
  if (has(/macd/)) {
    const candles = walk(seed, 15, 100, 1.8, 0.2)
    const rnd = seeded(seed + 'macd')
    const series: number[] = []
    const signal: number[] = []
    let v = -0.6
    let s = -0.6
    for (const _ of candles) {
      v += (rnd() - 0.35) * 0.5
      s = s * 0.7 + v * 0.3
      series.push(v)
      signal.push(s)
    }
    return {
      label: 'MACD · MACD line, signal line and histogram around zero',
      candles,
      panel: {
        series,
        signal,
        hist: series.map((x, i) => x - signal[i]),
        zero: true,
        min: -1.6,
        max: 1.6,
      },
    }
  }

  // RSI — momentum oscillator with 30/70 bands.
  if (has(/rsi|relative_?strength/)) {
    const rnd = seeded(seed + 'rsi')
    const candles = walk(seed, 15, 100, 1.8, 0.25)
    const series: number[] = []
    let v = 45
    for (let i = 0; i < candles.length; i++) {
      v += (rnd() - 0.4) * 12
      if (i === 8) v = 74 // push into overbought
      v = Math.max(12, Math.min(88, v))
      series.push(v)
    }
    return {
      label: 'RSI · momentum oscillator with 30 / 70 thresholds',
      candles,
      panel: { series, bands: [30, 70], min: 0, max: 100 },
    }
  }

  // Stochastic — %K / %D with 20/80.
  if (has(/stochastic|\bstoch\b|\bkd\b/)) {
    const rnd = seeded(seed + 'stoch')
    const candles = walk(seed, 15, 100, 1.8, 0.1)
    const k: number[] = []
    const d: number[] = []
    let v = 40
    let dv = 40
    for (const _ of candles) {
      v += (rnd() - 0.45) * 26
      v = Math.max(6, Math.min(94, v))
      dv = dv * 0.6 + v * 0.4
      k.push(v)
      d.push(dv)
    }
    return {
      label: 'Stochastic · %K and %D oscillator with 20 / 80 bands',
      candles,
      panel: { series: k, signal: d, bands: [20, 80], min: 0, max: 100 },
    }
  }

  // ADX — trend strength above a threshold.
  if (has(/\badx\b|dmi|trend_?strength/)) {
    const rnd = seeded(seed + 'adx')
    const candles = walk(seed, 15, 100, 1.8, 0.5)
    const series: number[] = []
    let v = 14
    for (let i = 0; i < candles.length; i++) {
      v += (rnd() * 0.8 + 0.4) * (i < 10 ? 1 : 0.2)
      series.push(Math.min(48, v))
    }
    return {
      label: 'ADX · trend strength rising through the threshold',
      candles,
      panel: { series, bands: [25], min: 0, max: 60 },
    }
  }

  // Slope / rate of change.
  if (has(/slope|\broc\b|rate_?of_?change|linreg|regression/)) {
    const candles = walk(seed, 15, 100, 1.6, 0.4)
    const series = candles.map((_, i) => -3 + (i / (candles.length - 1)) * 6)
    return {
      label: 'Slope · rate of change crossing zero',
      candles,
      panel: { series, zero: true, min: -4, max: 4 },
    }
  }

  // Generic momentum oscillator fallback.
  if (has(/oscill|momentum|cci|rvi|willr|mfi|ultimate/)) {
    const rnd = seeded(seed + 'osc')
    const candles = walk(seed, 15, 100, 1.8, 0.2)
    const series = candles.map(() => 20 + rnd() * 60)
    return {
      label: 'Oscillator · bounded momentum measure with guide bands',
      candles,
      panel: { series, bands: [30, 70], min: 0, max: 100 },
    }
  }

  // Generic signal fallback — an event marker on a reference level.
  const candles = walk(seed, 15, 100, 1.8, 0.2)
  return {
    label: 'Signal · fires when the condition is met on price',
    candles,
    hlines: [{ price: 101, dash: true }],
    markers: [
      { i: 6, dir: 'up', at: 'low' },
      { i: 11, dir: 'down', at: 'high' },
    ],
  }
}

// ── SVG renderer ─────────────────────────────────────────────────────────────

const W = 340
const H = 184

export function PrimitiveExample({
  pack,
  type,
  height = 184,
}: {
  pack: string
  type: string
  height?: number
}) {
  const sc = useMemo(() => buildScenario(pack, type), [pack, type])

  const g = useMemo(() => {
    const n = sc.candles.length
    const hasPanel = !!sc.panel
    const padX = 16
    const priceTop = 12
    const priceBottom = hasPanel ? 104 : 156
    const step = (W - padX * 2) / n
    const bodyW = Math.min(9, Math.max(2.5, step * 0.5))
    const cx = (i: number) => padX + step * i + step / 2

    let min = Infinity
    let max = -Infinity
    const see = (v: number | null | undefined) => {
      if (v == null || !isFinite(v)) return
      if (v < min) min = v
      if (v > max) max = v
    }
    for (const k of sc.candles) {
      see(k.h)
      see(k.l)
    }
    sc.boxes?.forEach((b) => {
      see(b.top)
      see(b.bottom)
    })
    sc.hlines?.forEach((h) => see(h.price))
    sc.overlays?.forEach((o) => o.values.forEach(see))
    const pad = (max - min || 1) * 0.08
    min -= pad
    max += pad
    const span = max - min || 1
    const y = (p: number) =>
      priceBottom - ((p - min) / span) * (priceBottom - priceTop)

    // Panel scale
    const panelTop = 122
    const panelBottom = 170
    let pMin = sc.panel?.min ?? 0
    let pMax = sc.panel?.max ?? 100
    if (sc.panel && sc.panel.min == null) {
      pMin = Infinity
      pMax = -Infinity
      const seeP = (v: number | null | undefined) => {
        if (v == null || !isFinite(v)) return
        if (v < pMin) pMin = v
        if (v > pMax) pMax = v
      }
      sc.panel.series.forEach(seeP)
      sc.panel.signal?.forEach(seeP)
      const pp = (pMax - pMin || 1) * 0.1
      pMin -= pp
      pMax += pp
    }
    const pySpan = pMax - pMin || 1
    const py = (v: number) =>
      panelBottom - ((v - pMin) / pySpan) * (panelBottom - panelTop)

    const poly = (vals: (number | null)[], map: (v: number) => number) =>
      vals
        .map((v, i) =>
          v == null ? null : `${cx(i).toFixed(1)},${map(v).toFixed(1)}`,
        )
        .filter(Boolean)
        .join(' ')

    return { n, cx, y, bodyW, py, panelTop, panelBottom, poly, hasPanel }
  }, [sc])

  return (
    <figure className="space-y-1.5">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="w-full rounded-md border border-border bg-muted/20"
        style={{ height }}
        role="img"
        aria-label={`Schematic example of ${humanize(type)}`}
      >
        {/* Session / time spans (behind everything) */}
        {sc.vspans?.map((v, i) => (
          <rect
            key={`v${i}`}
            x={g.cx(v.i1) - g.bodyW}
            y={10}
            width={g.cx(v.i2) - g.cx(v.i1) + g.bodyW * 2}
            height={g.hasPanel ? 96 : 148}
            fill={C.zone}
            fillOpacity={0.1}
            stroke={C.zone}
            strokeOpacity={0.4}
            strokeDasharray="3 3"
          />
        ))}

        {/* Zone boxes */}
        {sc.boxes?.map((b, i) => {
          const x = g.cx(b.i1) - g.bodyW
          const right = b.extend ? W - 6 : g.cx(b.i2) + g.bodyW
          return (
            <rect
              key={`b${i}`}
              x={x}
              y={g.y(b.top)}
              width={right - x}
              height={g.y(b.bottom) - g.y(b.top)}
              fill={b.color ?? C.zone}
              fillOpacity={0.16}
              stroke={b.color ?? C.zone}
              strokeOpacity={0.75}
              strokeWidth={1}
              strokeDasharray="4 3"
              rx={2}
            />
          )
        })}

        {/* Horizontal reference lines */}
        {sc.hlines?.map((h, i) => (
          <line
            key={`h${i}`}
            x1={10}
            x2={W - 8}
            y1={g.y(h.price)}
            y2={g.y(h.price)}
            stroke={h.color ?? C.muted}
            strokeOpacity={0.7}
            strokeWidth={1}
            strokeDasharray={h.dash === false ? undefined : '4 3'}
          />
        ))}

        {/* Candles (context candles dimmed, pattern candles solid) */}
        {sc.candles.map((k, i) => {
          const color = k.c >= k.o ? C.up : C.down
          const x = g.cx(i)
          const top = Math.min(g.y(k.o), g.y(k.c))
          const bh = Math.max(1.4, Math.abs(g.y(k.o) - g.y(k.c)))
          const op = k.em ? 1 : 0.5
          return (
            <g key={`c${i}`} opacity={op}>
              <line
                x1={x}
                x2={x}
                y1={g.y(k.h)}
                y2={g.y(k.l)}
                stroke={color}
                strokeWidth={1}
              />
              <rect
                x={x - g.bodyW / 2}
                y={top}
                width={g.bodyW}
                height={bh}
                fill={color}
                rx={0.5}
              />
            </g>
          )
        })}

        {/* Price overlays (MA lines, bands) */}
        {sc.overlays?.map((o, i) => (
          <polyline
            key={`o${i}`}
            points={g.poly(o.values, g.y)}
            fill="none"
            stroke={o.color ?? C.line}
            strokeWidth={1.6}
            strokeLinejoin="round"
            strokeLinecap="round"
          />
        ))}

        {/* Markers */}
        {sc.markers?.map((m, i) => {
          const x = g.cx(m.i)
          const k = sc.candles[m.i]
          if (m.dir === 'up') {
            const yy = g.y(m.at === 'low' ? k.l : k.h) + 9
            return (
              <path key={`m${i}`} d={`M${x} ${yy} l-4 6 h8 z`} fill={C.up} />
            )
          }
          const yy = g.y(m.at === 'high' ? k.h : k.l) - 9
          return (
            <path key={`m${i}`} d={`M${x} ${yy} l-4 -6 h8 z`} fill={C.down} />
          )
        })}

        {/* Oscillator sub-panel */}
        {sc.panel && (
          <g>
            <line
              x1={10}
              x2={W - 8}
              y1={g.panelTop - 8}
              y2={g.panelTop - 8}
              stroke={C.border}
              strokeWidth={1}
            />
            {sc.panel.zero && (
              <line
                x1={10}
                x2={W - 8}
                y1={g.py(0)}
                y2={g.py(0)}
                stroke={C.muted}
                strokeOpacity={0.6}
                strokeWidth={1}
              />
            )}
            {sc.panel.bands?.map((lvl, i) => (
              <line
                key={`pb${i}`}
                x1={10}
                x2={W - 8}
                y1={g.py(lvl)}
                y2={g.py(lvl)}
                stroke={C.zone}
                strokeOpacity={0.55}
                strokeWidth={1}
                strokeDasharray="3 3"
              />
            ))}
            {sc.panel.hist?.map((v, i) =>
              v == null ? null : (
                <rect
                  key={`ph${i}`}
                  x={g.cx(i) - g.bodyW / 2}
                  y={Math.min(g.py(0), g.py(v))}
                  width={g.bodyW}
                  height={Math.max(0.8, Math.abs(g.py(v) - g.py(0)))}
                  fill={v >= 0 ? C.up : C.down}
                  opacity={0.5}
                />
              ),
            )}
            {sc.panel.signal && (
              <polyline
                points={g.poly(sc.panel.signal, g.py)}
                fill="none"
                stroke={C.muted}
                strokeWidth={1.4}
                strokeLinejoin="round"
              />
            )}
            <polyline
              points={g.poly(sc.panel.series, g.py)}
              fill="none"
              stroke={C.line}
              strokeWidth={1.7}
              strokeLinejoin="round"
              strokeLinecap="round"
            />
          </g>
        )}
      </svg>
      <figcaption className="text-[11px] leading-tight text-muted-foreground">
        {sc.label} — illustrative, deterministic candles; not real data or a
        backtest.
      </figcaption>
    </figure>
  )
}

// ── Parameter schema table ───────────────────────────────────────────────────

function SchemaView({ schema }: { schema: string }) {
  const rows = useMemo(() => {
    if (!schema) return null
    try {
      const parsed = JSON.parse(schema) as Record<string, unknown>
      const props =
        parsed &&
        typeof parsed === 'object' &&
        'properties' in parsed &&
        parsed.properties
          ? (parsed.properties as Record<string, unknown>)
          : parsed
      const entries = Object.entries(props ?? {})
      if (!entries.length) return null
      return entries.map(([name, v]) => {
        const o =
          v && typeof v === 'object' ? (v as Record<string, unknown>) : {}
        return {
          name,
          type:
            typeof o.type === 'string'
              ? o.type
              : typeof v === 'string'
                ? v
                : '',
          def: o.default ?? o.example,
          desc: typeof o.description === 'string' ? o.description : '',
        }
      })
    } catch {
      return null
    }
  }, [schema])

  if (!schema)
    return (
      <p className="text-sm text-muted-foreground">
        This primitive takes no parameters.
      </p>
    )

  if (!rows) {
    return (
      <pre className="max-h-56 overflow-auto rounded-md border border-border bg-muted/30 p-3 text-xs">
        {schema}
      </pre>
    )
  }

  return (
    <div className="overflow-hidden rounded-md border border-border">
      <table className="w-full text-left text-sm">
        <thead className="bg-muted/40 text-xs uppercase tracking-wide text-muted-foreground">
          <tr>
            <th className="px-3 py-2 font-medium">Parameter</th>
            <th className="px-3 py-2 font-medium">Type</th>
            <th className="px-3 py-2 font-medium">Default</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.name} className="border-t border-border align-top">
              <td className="px-3 py-2">
                <span className="font-medium">{humanize(r.name)}</span>
                {r.desc && (
                  <p className="text-xs text-muted-foreground">{r.desc}</p>
                )}
              </td>
              <td className="px-3 py-2 font-mono text-xs text-muted-foreground">
                {r.type || '—'}
              </td>
              <td className="px-3 py-2 font-mono text-xs text-muted-foreground">
                {r.def === undefined || r.def === null ? '—' : String(r.def)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function PrimitiveInfo({ primitive }: { primitive: Primitive }) {
  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <h3 className="text-lg font-semibold leading-none tracking-tight">
          {humanize(primitive.type)}
        </h3>
        <p className="text-sm text-muted-foreground">
          {humanize(primitive.pack)} · outputs {primitive.outputKind}
          {primitive.timeframed ? ' · timeframe-aware' : ''}
        </p>
      </div>
      <PrimitiveExample pack={primitive.pack} type={primitive.type} />
      <div className="space-y-2">
        <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Parameters
        </p>
        <SchemaView schema={primitive.parameterSchema} />
      </div>
    </div>
  )
}

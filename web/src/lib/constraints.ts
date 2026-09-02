/**
 * Product constraints surfaced in the UI. These mirror what the local server
 * enforces; showing them keeps the interface honest about what a run can and
 * cannot do rather than letting the user discover limits through errors.
 */

export const DEFAULT_CACHE_MAX_MB = 5120 // 5 GiB default eviction budget.

export interface Constraint {
  title: string
  detail: string
}

export const CONSTRAINTS: Constraint[] = [
  {
    title: 'One instrument per run',
    detail:
      'Current execution backtests a single instrument at a time. Compose multi-instrument studies by running each separately and comparing.',
  },
  {
    title: 'Timeframe availability',
    detail:
      'Crypto supports M15 and above. FX supports M1, M5, H1, H4, D1, and W1 from the local Dukascopy-backed cache. Trade simulations use M5 or M1; broader bars remain useful for impact research.',
  },
  {
    title: 'Local cache capped at 5 GiB',
    detail:
      'Bars are cached locally as month-partitioned Parquet with an oldest-file eviction budget (5 GiB default). Preparing large windows may evict older data.',
  },
  {
    title: 'Dates are epoch milliseconds',
    detail:
      'Time windows are [from, to) in epoch ms. The generated client uses bigint for fromMs/toMs — values are never floating point.',
  },
]

/** Canonical ordering of timeframes from finest to coarsest (display fallback). */
export const TIMEFRAME_ORDER = [
  'M1',
  'M5',
  'M15',
  'M30',
  'H1',
  'H4',
  'D1',
  'W1',
] as const

// Minutes per unit letter. MT-style: M = minute, H = hour, D = day, W = week,
// MN/MO = month. Note the deliberate distinction between "M" (minute) and
// "MN"/"MO" (month).
const UNIT_MINUTES: Partial<Record<string, number>> = {
  S: 1 / 60,
  SEC: 1 / 60,
  T: 1 / 60,
  M: 1,
  MIN: 1,
  H: 60,
  HR: 60,
  HOUR: 60,
  D: 1440,
  DAY: 1440,
  W: 10080,
  WK: 10080,
  WEEK: 10080,
  MN: 43200,
  MO: 43200,
  MON: 43200,
  MONTH: 43200,
}

/**
 * Parse a timeframe code into minutes, tolerating the common shapes:
 * "M15", "15M", "15m", "H4", "4h", "D1", "1d", "W1", "1w", "MN1", "1MO".
 * Returns null when the code can't be understood.
 */
export function timeframeMinutes(tf: string): number | null {
  const s = tf.trim().toUpperCase()
  if (!s) return null

  // Whole-word month tokens before the generic parse (avoid "M" = minute).
  if (s === 'MN' || s === 'MON' || s === 'MONTH' || s === 'MO') return 43200

  // letter(s)+number, e.g. M15, H4, MN1
  let unit: string | undefined
  let n: number | undefined
  let m = s.match(/^([A-Z]+)(\d+)$/)
  if (m) {
    unit = m[1]
    n = Number(m[2])
  } else {
    // number+letter(s), e.g. 15M, 4H, 1D, 1MO
    m = s.match(/^(\d+)([A-Z]+)$/)
    if (m) {
      n = Number(m[1])
      unit = m[2]
    }
  }
  if (unit === undefined || n === undefined) return null
  const per = UNIT_MINUTES[unit]
  if (per === undefined) return null
  return per * n
}

/** Sort a set of timeframe codes from finest to coarsest by real duration. */
export function sortTimeframes(timeframes: readonly string[]): string[] {
  return [...timeframes].sort((a, b) => {
    const ma = timeframeMinutes(a)
    const mb = timeframeMinutes(b)
    // Unparseable codes sort to the end but keep a stable relative order.
    if (ma === null && mb === null) return a.localeCompare(b)
    if (ma === null) return 1
    if (mb === null) return -1
    return ma - mb
  })
}

/** True when a timeframe is M15 (15 minutes) or coarser — the crypto floor. */
export function isM15OrAbove(tf: string): boolean {
  const m = timeframeMinutes(tf)
  return m !== null && m >= 15
}

/**
 * DUKAS FX is fetched directly into the local cache from native tick archives.
 */
export function isDukasFxInstrument(symbol: string): boolean {
  return /^DUKAS:/i.test(symbol.trim())
}

/** Whether an advertised interval can be prepared by the selected source. */
export function isTimeframeSupportedForInstrument(
  symbol: string,
  timeframe: string,
): boolean {
  if (assetClassHint(symbol) === 'crypto') return isM15OrAbove(timeframe)
  if (!isDukasFxInstrument(symbol)) return true
  const minutes = timeframeMinutes(timeframe)
  return (
    minutes !== null && [1, 5, 15, 30, 60, 240, 1440, 10080].includes(minutes)
  )
}

/** Preserve the server's timeframe spelling while removing incompatible ones. */
export function supportedTimeframesForInstrument(
  symbol: string,
  timeframes: readonly string[],
): string[] {
  return timeframes.filter((timeframe) =>
    isTimeframeSupportedForInstrument(symbol, timeframe),
  )
}

/**
 * Rough asset-class guess from a quality tier or symbol, used only to render an
 * advisory note. The instrument's advertised `timeframes` remain authoritative.
 */
export function assetClassHint(
  symbol: string,
  qualityTier?: string,
): 'crypto' | 'fx' | 'unknown' {
  const t = (qualityTier ?? '').toLowerCase()
  if (t.includes('crypto')) return 'crypto'
  if (t.includes('fx') || t.includes('forex')) return 'fx'
  const s = symbol.toUpperCase()
  if (/(BTC|ETH|SOL|USDT|USDC|XBT)/.test(s)) return 'crypto'
  if (isDukasFxInstrument(s) || /^[A-Z]{6}$/.test(s) || s.includes('/'))
    return 'fx'
  return 'unknown'
}

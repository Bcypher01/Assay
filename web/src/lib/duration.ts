/**
 * Durations, in the exact shape the Go engine speaks.
 *
 * `core.Duration` (see internal/core/duration.go) is a millisecond span whose
 * serialised form is a single unit-suffixed token: "45m", "4h", "3d". A bare
 * number is refused on the wire, because guessing milliseconds when the author
 * meant minutes is wrong by 60,000x and silent. This module is the browser
 * mirror of that type: it must format and parse identically so a window
 * authored here round-trips through Validate without surprise.
 *
 * Window operators (within / persists / after) moved from bar counts to
 * durations the moment nodes could sit on different timeframes: "within 3 bars
 * of the H4 point of interest" is either 12 hours or 45 minutes, a factor of
 * 16, and both readings are what someone meant. Authoring a window therefore
 * means picking a bar count *and* the timeframe it is counted in, and letting
 * this module convert to the unambiguous duration the engine stores.
 */

/** Milliseconds per timeframe bar, matching internal/core/timeframe.go exactly. */
export const TIMEFRAME_MS: Record<string, number> = {
  m1: 60_000,
  m5: 5 * 60_000,
  m15: 15 * 60_000,
  m30: 30 * 60_000,
  h1: 60 * 60_000,
  h4: 4 * 60 * 60_000,
  d1: 24 * 60 * 60_000,
  w1: 7 * 24 * 60 * 60_000,
}

const SECOND = 1000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR
const WEEK = 7 * DAY

// Largest unit first, so 14_400_000 renders as "4h" rather than "240m" — the
// same canonical choice Go's Duration.String makes.
const UNITS: Array<[string, number]> = [
  ['w', WEEK],
  ['d', DAY],
  ['h', HOUR],
  ['m', MINUTE],
  ['s', SECOND],
  ['ms', 1],
]

/**
 * Canonical string for a millisecond span: the largest whole unit that divides
 * it exactly. Zero renders as "0"; a negative span keeps its sign so a bad
 * value stays visible rather than being silently coerced.
 */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms)) return '0'
  const n = Math.trunc(ms)
  if (n === 0) return '0'
  const sign = n < 0 ? '-' : ''
  const abs = Math.abs(n)
  for (const [suffix, span] of UNITS) {
    if (abs % span === 0) return `${sign}${abs / span}${suffix}`
  }
  return `${sign}${abs}ms`
}

/**
 * Parse a canonical duration back to milliseconds. Accepts a single
 * unit-suffixed token (w, d, h, m, s, ms); a compound form ("1h30m") is
 * rejected, exactly as core.ParseDuration rejects it, and so is a bare number.
 * Returns null on anything unparseable.
 */
export function parseDuration(value: string): number | null {
  const s = value.trim()
  if (s === '0') return 0
  // Order matters: try "ms" before "m"/"s" so "10ms" is not read as "10m".
  for (const [suffix, span] of [...UNITS].sort(
    (a, b) => b[0].length - a[0].length,
  )) {
    if (!s.endsWith(suffix)) continue
    const digits = s.slice(0, -suffix.length)
    if (!/^-?\d+$/.test(digits)) continue
    return Number(digits) * span
  }
  return null
}

/** Milliseconds in one bar of a timeframe, or null for an unknown code. */
export function timeframeMs(tf: string): number | null {
  return TIMEFRAME_MS[tf] ?? null
}

/**
 * Convert a bar count on a given timeframe to a canonical duration string.
 * Returns null when the timeframe is unknown or the count is not positive, so
 * the caller can leave the window unset rather than emit "0".
 */
export function barsToDuration(bars: number, tf: string): string | null {
  const per = timeframeMs(tf)
  if (per === null || !Number.isFinite(bars) || bars <= 0) return null
  return formatDuration(Math.round(bars) * per)
}

/**
 * Best-effort inverse: how many whole bars of a timeframe a duration spans.
 * Used only to seed a bar-count input from a stored duration; the stored
 * duration remains the source of truth.
 */
export function durationToBars(value: string, tf: string): number | null {
  const ms = parseDuration(value)
  const per = timeframeMs(tf)
  if (ms === null || per === null || per === 0) return null
  return Math.max(1, Math.round(ms / per))
}

/** A short human phrase for a stored duration, e.g. "12h", used in readings. */
export function displayDuration(value: string): string {
  const ms = parseDuration(value)
  return ms === null ? value : formatDuration(ms)
}

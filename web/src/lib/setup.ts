/**
 * The Setup Test wire contract.
 *
 * This surface is deliberately plain JSON rather than ConnectRPC: the server
 * exposes `/api/setup/{chart,preview,run}` as ordinary POST endpoints so a
 * drawing can be evaluated before a strategy document exists. The types here
 * mirror `internal/setuptest` field for field — the Go decoder is configured
 * with `DisallowUnknownFields`, so an extra property in a request body is a
 * 400, not a silently ignored key.
 *
 * Every price is instrument ticks (`core.Price`, an int64 scaled by the
 * instrument's tick size). Ticks are integers everywhere in this module.
 * Floating point appears only where a value is displayed, never where one is
 * compared or sent.
 */

import { assayHttpBaseUrl } from '@/lib/rpc'

export type SetupKind =
  | 'drawn_precedent'
  | 'break_retest'
  | 'order_block_retest'
  | 'breaker_retest'
  | 'fair_value_gap_retest'
export type SetupSide = 'long' | 'short'

export interface PrecedentBar {
  openMilli: number
  highMilli: number
  lowMilli: number
  closeMilli: number
}

/** The complete user-confirmed meaning of one annotated setup. */
export interface Drawing {
  kind: SetupKind
  side: SetupSide
  symbol: string
  timeframe: string
  anchorTimeMs: number
  levelLow: number
  levelHigh: number
  entry: number
  stop: number
  target: number
  lookbackBars: number
  retestBars: number
  toleranceTicks: number
  expiryBars: number
  displacementBodyMilli: number
  useBody: boolean
  confirmByClose: boolean
  minSizeTicks: number
  mitigationRule: 'first_touch' | 'half_fill' | 'full_fill'
  riskUnit: 'ticks' | 'percent'
  riskPerTrade: number
  precedent: Array<PrecedentBar>
  similarityMilli: number
}

export interface SetupBar {
  timeMs: number
  open: number
  high: number
  low: number
  close: number
  volume: number
}

export interface SetupChart {
  symbol: string
  timeframe: string
  /** Decimal string, e.g. "0.00001". The tick size is never a float here. */
  tickSize: string
  pipTicks: number
  snapshot: string
  fromMs: number
  toMs: number
  bars: Array<SetupBar>
}

export interface SetupMatch {
  signalTimeMs: number
  entryTimeMs: number
  exitTimeMsPessimistic: number
  exitTimeMsOptimistic: number
  entry: number
  exitPessimistic: number
  exitOptimistic: number
  stop: number
  target: number
  levelLow: number
  levelHigh: number
  pessimisticPnl: number
  optimisticPnl: number
  ambiguous: boolean
  similarityMilli?: number
  shapeMilli?: number
  structureMilli?: number
  directionMilli?: number
  anatomyMilli?: number
  triggerMilli?: number
}

/**
 * Ambiguity-affected metrics arrive as a pessimistic/optimistic pair. The
 * pessimistic figure is the one the interface leads with; a backtester that
 * shows the favourable branch of an unresolved bar is lying by omission.
 */
export interface Bounded {
  Pessimistic: number
  Optimistic: number
}

/** `analytics.Metrics` has no JSON tags, so it serialises with Go field names. */
export interface SetupMetrics {
  FinalEquity?: Bounded
  MaxDrawdown?: Bounded
  AverageDrawdown?: Bounded
  ProfitFactor?: Bounded
  Expectancy?: Bounded
  Trades?: number
  Wins?: number
  Losses?: number
  LongestLosingStreak?: number
}

export interface SetupPreview {
  snapshot: string
  strategyJson: unknown
  strategyHash: string
  interpretation: string
  sampleMatched: boolean
  ruleMatched: boolean
  drawingMatched: boolean
  matches: Array<SetupMatch>
  metrics: SetupMetrics
  unresolved: number
  warnings?: Array<string>
}

export interface SetupAssumptions {
  initialEquity: number
  riskPerTrade: number
  spreadTicks: number
  slippageTicks: number
}

/** `assayv1.RunSummary` marshalled with encoding/json: omitempty drops zeroes. */
export interface SetupRunSummary {
  run_id?: string
  trades?: number
  unresolved?: number
  wins?: number
  losses?: number
  net_pnl?: number
  error?: string
  error_code?: string
}

/**
 * A failed setup call. The server answers with `{"error": "..."}` and a status;
 * carrying the status lets a screen tell "you have not built the server yet"
 * (501) apart from "your drawing is invalid" (400).
 */
export class SetupError extends Error {
  readonly status: number
  constructor(message: string, status: number) {
    super(message)
    this.name = 'SetupError'
    this.status = status
  }
}

/** True when the local server predates the Setup Test endpoints. */
export function isSetupUnavailable(err: unknown): boolean {
  return err instanceof SetupError && (err.status === 501 || err.status === 404)
}

export type SetupOperation = 'prepare' | 'test' | 'save'

/**
 * Convert transport and server diagnostics into useful product language.
 * The raw error remains on the Error object for developer logging, but API
 * paths, JSON decoder messages, HTTP codes and Go field names never reach the
 * trader-facing interface.
 */
export function friendlySetupError(
  error: unknown,
  operation: SetupOperation,
): string {
  const fallback =
    operation === 'prepare'
      ? 'The chart could not be prepared. Check the selected market and try again.'
      : operation === 'save'
        ? 'The strategy could not be saved. Your setup is still here; try again.'
        : 'The setup test could not be completed. Review the setup and try again.'

  if (!(error instanceof Error)) return fallback

  const raw = error.message.toLowerCase()
  const status = error instanceof SetupError ? error.status : undefined

  if (
    status === 0 ||
    raw.includes('failed to fetch') ||
    raw.includes('could not reach') ||
    raw.includes('networkerror')
  ) {
    return 'Assay is not connected. Start or restart the local Assay server, then try again.'
  }
  if (status === 404 || status === 501) {
    return 'Setup testing is not available in the running Assay version. Restart Assay after installing the latest build.'
  }
  if (
    raw.includes('unknown field') ||
    raw.includes('decode setup request') ||
    raw.includes('invalid character') ||
    raw.includes('json:')
  ) {
    return 'The page and the local Assay server are out of sync. Refresh the page and restart Assay, then try again.'
  }
  if (
    raw.includes('snapshot') ||
    raw.includes('prepared series does not match') ||
    status === 409 ||
    status === 412
  ) {
    return 'The chart data changed while this setup was open. Reload the chart and try again.'
  }
  if (
    raw.includes('data missing') ||
    raw.includes('not prepared') ||
    raw.includes('missing partition') ||
    raw.includes('no bars') ||
    raw.includes('no candles')
  ) {
    return 'Price history is not ready for this selection. Prepare or reload the chart, then try again.'
  }
  if (
    raw.includes('fallback fetch') ||
    raw.includes('dukascopy:') ||
    raw.includes('download failed') ||
    raw.includes('service unavailable') ||
    raw.includes('status 503')
  ) {
    return 'Price data is temporarily unavailable for this market. Wait a moment, then try this market again.'
  }
  if (status === 400 || raw.includes('validation')) {
    return 'Some setup details need attention. Check the selected candle, entry, stop and target, then try again.'
  }
  if (status === 429) {
    return 'Assay is already handling several tests. Wait a moment, then try again.'
  }
  if (status !== undefined && status >= 500) {
    return 'Assay could not complete that request. Your setup is unchanged; try again in a moment.'
  }
  return fallback
}

/**
 * POST to a setup endpoint.
 *
 * `assayHttpBaseUrl` is "/api" in dev, and the Vite proxy strips exactly one
 * leading "/api" before forwarding, so "/api" + "/api/setup/chart" arrives at
 * the server as "/api/setup/chart". Pointing VITE_ASSAY_BASE_URL straight at
 * the server resolves to the same path without a proxy. Both work; neither
 * needs a special case here.
 */
async function post<T>(path: string, body: unknown): Promise<T> {
  let response: Response
  try {
    response = await fetch(`${assayHttpBaseUrl}${path}`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    })
  } catch (cause) {
    throw new SetupError(
      `Could not reach the local Assay server. ${String(cause)}`,
      0,
    )
  }
  const payload: unknown = await response.json().catch(() => null)
  if (!response.ok) {
    const message =
      payload &&
      typeof payload === 'object' &&
      'error' in payload &&
      typeof payload.error === 'string'
        ? payload.error
        : response.statusText || `HTTP ${response.status}`
    throw new SetupError(message, response.status)
  }
  return payload as T
}

export interface ChartRequest {
  symbol: string
  timeframe: string
  fromMs: number
  toMs: number
  maxBars: number
  /** Fetch and cache the bars if they are not already local. */
  prepare: boolean
}

export function fetchSetupChart(request: ChartRequest): Promise<SetupChart> {
  return post<SetupChart>('/api/setup/chart', request)
}

export function previewSetup(
  drawing: Drawing,
  fromMs: number,
  toMs: number,
): Promise<SetupPreview> {
  return post<SetupPreview>('/api/setup/preview', { drawing, fromMs, toMs })
}

export function runSetup(
  drawing: Drawing,
  fromMs: number,
  toMs: number,
  assumptions: SetupAssumptions,
): Promise<SetupRunSummary> {
  return post<SetupRunSummary>('/api/setup/run', {
    drawing,
    fromMs,
    toMs,
    assumptions,
  })
}

/* ------------------------------------------------------------------ prices */

/** Decimal places implied by a tick-size string ("0.00001" -> 5). */
export function tickDigits(tickSize: string): number {
  return (tickSize.split('.')[1] ?? '').length
}

/**
 * Render a tick count as an instrument price.
 *
 * Multiplying by the tick size is a display step and the only place a float is
 * allowed near a price. The result is fixed to the tick size's own precision,
 * so no rounding artefact ever reaches the screen.
 */
export function formatTicks(ticks: number, tickSize: string): string {
  const size = Number(tickSize)
  if (!Number.isFinite(size) || size === 0) return String(ticks)
  return (ticks * size).toFixed(tickDigits(tickSize))
}

/** Reward-to-risk of a drawing, or null when the risk leg is degenerate. */
export function riskReward(drawing: {
  entry: number
  stop: number
  target: number
}): number | null {
  const risk = Math.abs(drawing.entry - drawing.stop)
  if (risk === 0) return null
  return Math.abs(drawing.target - drawing.entry) / risk
}

/* ------------------------------------------------------------ vocabulary */

export const SETUP_KINDS: Array<{
  id: SetupKind
  label: string
  blurb: string
  /** What the engine actually repeats, in the user's own words. */
  rule: string
}> = [
  {
    id: 'drawn_precedent',
    label: 'Drawn precedent',
    blurb: 'The normalized candle shape you selected directly on the chart.',
    rule: 'price action matching the candle sequence inside your drawn precedent',
  },
  {
    id: 'fair_value_gap_retest',
    label: 'Fair value gap',
    blurb: 'A three-candle imbalance followed by the selected return.',
    rule: 'a three-candle fair value gap, then the confirmed fill of that gap',
  },
  {
    id: 'break_retest',
    label: 'Break + retest',
    blurb: 'A prior extreme breaks, then price returns to it.',
    rule: 'a break of the preceding extreme, then a return to the broken level',
  },
  {
    id: 'order_block_retest',
    label: 'Order block',
    blurb: 'Last opposing candle before displacement.',
    rule: 'the first return to the last opposing candle before a strong move',
  },
  {
    id: 'breaker_retest',
    label: 'Breaker block',
    blurb: 'An opposing zone flips, then gets retested.',
    rule: 'the first return to an opposing zone after it flips side',
  },
]

/** Bounds mirrored from `Drawing.Validate` so the UI fails before the server. */
export const SETUP_LIMITS = {
  lookbackBars: { min: 2, max: 500 },
  retestBars: { min: 1, max: 200 },
  expiryBars: { min: 1, max: 1000 },
  toleranceTicks: { min: 0, max: 10_000 },
} as const

/**
 * Client-side validation, in the same order and with the same messages as the
 * Go validator. Keyed by field so a screen can render each message beside the
 * control that caused it, which is the house rule for validation errors.
 */
export function validateDrawing(d: Drawing): Record<string, string> {
  const errors: Record<string, string> = {}
  if (d.anchorTimeMs <= 0) errors.anchor = 'Select a setup candle on the chart.'
  if (d.levelLow <= 0 || d.levelHigh <= 0 || d.levelLow > d.levelHigh)
    errors.level = 'Draw a valid level or zone.'
  if (d.side === 'long' && !(d.stop < d.entry && d.entry < d.target))
    errors.stop = 'A long setup needs stop below entry and target above it.'
  if (d.side === 'short' && !(d.target < d.entry && d.entry < d.stop))
    errors.stop = 'A short setup needs stop above entry and target below it.'
  if (
    d.lookbackBars < SETUP_LIMITS.lookbackBars.min ||
    d.lookbackBars > SETUP_LIMITS.lookbackBars.max
  )
    errors.lookbackBars = 'Must be between 2 and 500 bars.'
  if (
    d.retestBars < SETUP_LIMITS.retestBars.min ||
    d.retestBars > SETUP_LIMITS.retestBars.max
  )
    errors.retestBars = 'Must be between 1 and 200 bars.'
  if (d.toleranceTicks < 0) errors.toleranceTicks = 'Must not be negative.'
  if (d.kind === 'fair_value_gap_retest' && d.minSizeTicks < 1)
    errors.minSizeTicks = 'Must be at least 1 tick.'
  if (
    d.kind === 'drawn_precedent' &&
    (d.precedent.length < 3 || d.precedent.length > 64)
  )
    errors.precedent = 'Draw an example spanning between 3 and 64 candles.'
  if (
    d.kind === 'drawn_precedent' &&
    (d.similarityMilli < 500 || d.similarityMilli > 1000)
  )
    errors.similarityMilli = 'Similarity must be between 50% and 100%.'
  if (
    d.expiryBars < SETUP_LIMITS.expiryBars.min ||
    d.expiryBars > SETUP_LIMITS.expiryBars.max
  )
    errors.expiryBars = 'Must be between 1 and 1000 bars.'
  if (d.riskPerTrade <= 0 || d.riskPerTrade > 0.1)
    errors.riskPerTrade = 'Must be above 0% and no more than 10%.'
  return errors
}

/* --------------------------------------------------------- data quality */

/**
 * What a loaded chart looks like structurally, independent of what it claims
 * to be.
 *
 * Assay's whole argument is that a backtest is only worth what its data is
 * worth, so a series that cannot support the question being asked has to say
 * so on the screen rather than in a footnote. These are cheap structural
 * checks on the bars actually returned — no opinion about the venue, just
 * arithmetic that a real price series would pass.
 */
export interface BarDiagnostics {
  /** Median |open − close| as a percentage of the bar's own range. */
  medianBodyPercent: number
  /** Bars whose open and close are identical to the tick. */
  flatBars: number
  /** Bars opening on a Saturday or Sunday in UTC. */
  weekendBars: number
  /** Bars reporting no volume at all. */
  zeroVolumeBars: number
  reasons: Array<string>
}

/**
 * A real daily FX series puts something like a fifth to a half of each bar's
 * range into its body. A series whose median body is a few percent is not
 * describing an open and a close; it is reporting the same quote twice. That
 * matters here beyond appearance: the semantic detectors measure displacement
 * as a share of body, so body-based setups cannot form on such a series at
 * all, and an empty result would look like an answer.
 */
const SUSPECT_BODY_PERCENT = 8

export function inspectBars(chart: SetupChart | null): BarDiagnostics | null {
  if (!chart || chart.bars.length < 10) return null
  const percents: Array<number> = []
  let flatBars = 0
  let weekendBars = 0
  let zeroVolumeBars = 0
  for (const bar of chart.bars) {
    const range = bar.high - bar.low
    const body = Math.abs(bar.open - bar.close)
    if (range > 0) percents.push((body / range) * 100)
    if (body === 0) flatBars += 1
    if (bar.volume === 0) zeroVolumeBars += 1
    const weekday = new Date(bar.timeMs).getUTCDay()
    if (weekday === 0 || weekday === 6) weekendBars += 1
  }
  percents.sort((a, b) => a - b)
  const medianBodyPercent =
    percents.length === 0 ? 0 : percents[Math.floor(percents.length / 2)]

  const reasons: Array<string> = []
  if (medianBodyPercent < SUSPECT_BODY_PERCENT) {
    reasons.push(
      `The median candle body is ${medianBodyPercent.toFixed(1)}% of its own high-to-low range. Opens and closes this close together are a reporting artefact, not a market: setups that depend on candle bodies cannot form on this series.`,
    )
  }
  if (weekendBars > 0) {
    reasons.push(
      `${weekendBars} ${weekendBars === 1 ? 'bar opens' : 'bars open'} on a Saturday or Sunday. The trading week for this instrument runs Sunday 17:00 to Friday 17:00 New York, so these bars are dated against a different convention than a broker chart uses.`,
    )
  }
  return {
    medianBodyPercent,
    flatBars,
    weekendBars,
    zeroVolumeBars,
    reasons,
  }
}

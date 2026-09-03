import { fromJsonBytes } from '@/lib/codec'
import { describeLocally } from '@/lib/describe'
import type { LocalDescription } from '@/lib/describe'
import { parseStrategy, SCHEMA_V2 } from '@/lib/strategy'
import type { StrategyDoc, StrategyNode } from '@/lib/strategy'
import { isUnimplemented, rpcErrorMessage, templateApi } from '@/lib/rpc'

/**
 * Starting points.
 *
 * The server owns the real catalogue (`TemplateService.ListTemplates`) and it
 * is always preferred. These bundled copies mirror it so a first run still has
 * somewhere to start when the local server predates the endpoint — an empty
 * gallery reads as a broken product rather than an old binary.
 *
 * Every template composes boolean primitives only, because a slot accepts
 * nothing else. An RSI or Bollinger template would need a numeric primitive
 * compared against a threshold; those arrive with the predicate layer, and
 * advertising them before then would mean testing something other than the
 * rule shown on the card.
 */

export interface TemplateCard {
  id: string
  name: string
  family: string
  description: string
  expectedFrequency: string
  document: StrategyDoc
  described: LocalDescription
  /** Where the catalogue came from, so the UI can say when it is a stand-in. */
  source: 'server' | 'bundled'
}

function node(
  id: string,
  pack: string,
  type: string,
  params: Record<string, unknown>,
  timeframe: string,
): StrategyNode {
  return { id, pack, type, params, timeframe }
}

interface Blueprint {
  id: string
  name: string
  family: string
  description: string
  expectedFrequency: string
  symbol: string
  timeframe: string
  stop: StrategyDoc['risk']['stop']
  target: StrategyDoc['risk']['target']
  bias?: StrategyNode[]
  poi?: StrategyNode[]
  trigger: StrategyNode[]
  withinBars?: number
  /** Per-rung timeframes for a genuine ladder; defaults to `timeframe`. */
  ladder?: { bias: string; poi: string; entry: string }
  /**
   * A level node the entry compares price against, rather than a boolean the
   * entry simply requires. The node is stamped with `level.timeframe` when it
   * is given, so a daily reference can sit under a finer entry chart.
   */
  level?: {
    node: StrategyNode
    timeframe?: string
    source: 'open' | 'high' | 'low' | 'close'
    relation: 'above' | 'below' | 'crosses_above' | 'crosses_below'
  }
  /** Hold the point of interest live for this duration (e.g. "12h"). */
  poiWindow?: string
  /** Template version; bump when the rule structure changes. Defaults to 1. */
  version?: number
}

function build(b: Blueprint): TemplateCard {
  const biasTf = b.ladder?.bias ?? b.timeframe
  const poiTf = b.ladder?.poi ?? b.timeframe
  const entryTf = b.ladder?.entry ?? b.timeframe
  const stamp = (nodes: StrategyNode[] = [], tf: string) =>
    nodes.map((n) => ({ ...n, timeframe: tf }))
  const bias = stamp(b.bias, biasTf)
  const poi = stamp(b.poi, poiTf)
  const trigger = stamp(b.trigger, entryTf)
  const ids = (nodes: StrategyNode[]) => nodes.map((n) => n.id)
  const level = b.level
    ? { ...b.level.node, timeframe: b.level.timeframe ?? entryTf }
    : null

  // A point of interest can stay live for a window: a coarse level remains
  // valid across the finer bars beneath it. It is stored as the duration the
  // engine reads, wrapping the reference in an `occurred within`.
  const ref = (id: string, held?: string) =>
    held ? { within: { condition: { node: id }, window: held } } : { node: id }

  const document: StrategyDoc = {
    schema: SCHEMA_V2,
    name: b.name,
    universe: { symbols: [b.symbol] },
    timeframes: { bias: biasTf, poi: poiTf, entry: entryTf },
    risk: { stop: b.stop, target: b.target },
    template: { id: b.id, version: b.version ?? 1 },
    nodes: [...bias, ...poi, ...trigger, ...(level ? [level] : [])],
    entries: {
      timing: 'next_open',
      long: {
        enabled: true,
        name: `Long ${b.name}`,
        when: {
          all: [
            ...ids(bias).map((id) => ref(id)),
            ...ids(poi).map((id) => ref(id, b.poiWindow)),
            ...ids(trigger).map((id) => ref(id)),
            ...(level && b.level
              ? [
                  {
                    price: {
                      node: level.id,
                      source: b.level.source,
                      relation: b.level.relation,
                    },
                  },
                ]
              : []),
          ],
        },
      },
      short: { enabled: false, name: '', when: {} },
    },
  }

  return {
    id: b.id,
    name: b.name,
    family: b.family,
    description: b.description,
    expectedFrequency: b.expectedFrequency,
    document,
    described: describeLocally(document),
    source: 'bundled',
  }
}

export const BUNDLED_TEMPLATES: TemplateCard[] = [
  build({
    id: 'ma-crossover',
    name: '20/50 moving-average crossover',
    family: 'Trend following',
    description:
      'Catches sustained directional moves, and gives a good part of each one back at the turn. The clearest place to start, because there is almost nothing to fit.',
    expectedFrequency: 'low',
    symbol: 'DUKAS:EURUSD',
    timeframe: 'h4',
    stop: { unit: 'pips', value: 30 },
    target: { unit: 'pips', value: 60 },
    trigger: [
      node(
        'cross',
        'indicator',
        'ma_cross_up',
        { fastPeriod: 20, slowPeriod: 50 },
        'h4',
      ),
    ],
  }),
  build({
    id: 'trend-pullback',
    name: 'Pullback to the 20-period average',
    family: 'Trend following',
    description:
      'Waits for price to come back to the average inside a trend, then requires a candle that closes the other way before committing.',
    expectedFrequency: 'medium',
    symbol: 'DUKAS:EURUSD',
    timeframe: 'h1',
    stop: { unit: 'pips', value: 20 },
    target: { unit: 'pips', value: 40 },
    withinBars: 3,
    poi: [
      node(
        'touch',
        'indicator',
        'ma_touch',
        { period: 20, toleranceTicks: 30 },
        'h1',
      ),
    ],
    trigger: [
      node(
        'engulf',
        'priceaction',
        'engulfing',
        { period: 1, enabled: true },
        'h1',
      ),
    ],
  }),
  build({
    id: 'range-break',
    name: 'Range compression breakout',
    family: 'Price action',
    description:
      'Finds a stretch where price has held a tight range, then enters on the first candle with enough force to leave it.',
    expectedFrequency: 'low',
    symbol: 'DUKAS:GBPUSD',
    timeframe: 'h1',
    stop: { unit: 'pips', value: 25 },
    target: { unit: 'pips', value: 50 },
    withinBars: 2,
    bias: [
      node(
        'range',
        'priceaction',
        'consolidation',
        { period: 12, maxWidthTicks: 400 },
        'h1',
      ),
    ],
    trigger: [
      node(
        'push',
        'smc',
        'displacement',
        { minBodyRatioMilli: 700, minConsecutiveBars: 1, requireGap: false },
        'h1',
      ),
    ],
  }),
  build({
    id: 'session-break',
    name: 'London session breakout',
    family: 'Price action',
    description:
      'Trades only inside the London session, and only when a candle closes with real force behind it.',
    expectedFrequency: 'medium',
    symbol: 'DUKAS:GBPUSD',
    timeframe: 'm15',
    stop: { unit: 'pips', value: 18 },
    target: { unit: 'pips', value: 36 },
    bias: [
      node(
        'session',
        'timeofday',
        'session',
        { startMinuteUtc: 420, endMinuteUtc: 660 },
        'm15',
      ),
    ],
    trigger: [
      node(
        'push',
        'smc',
        'displacement',
        { minBodyRatioMilli: 650, minConsecutiveBars: 1, requireGap: false },
        'm15',
      ),
    ],
  }),
  build({
    id: 'ladder-daily-h4-m15',
    name: 'Daily bias, H4 sweep, M15 shift',
    family: 'Multi-timeframe',
    description:
      'A genuine ladder. The daily chart sets the structural bias, a 4-hour liquidity sweep marks the level and holds it live for twelve hours, and a 15-minute structure shift is the trigger. Each rung is read at its own close and projected down to the entry chart.',
    expectedFrequency: 'low',
    symbol: 'DUKAS:EURUSD',
    timeframe: 'm15',
    ladder: { bias: 'd1', poi: 'h4', entry: 'm15' },
    poiWindow: '12h',
    stop: { unit: 'pips', value: 15 },
    target: { unit: 'pips', value: 45 },
    bias: [
      node(
        'daily-structure',
        'smc',
        'market_structure',
        {
          swingLookback: 5,
          confirmByClose: true,
          equalLevelToleranceTicks: 20,
          trackInternal: false,
        },
        'd1',
      ),
    ],
    poi: [
      node(
        'h4-sweep',
        'smc',
        'liquidity_sweep',
        {
          wickThroughTicks: 40,
          closeBackBars: 2,
          requireStructureShift: false,
        },
        'h4',
      ),
    ],
    trigger: [
      node(
        'm15-shift',
        'smc',
        'market_structure',
        {
          swingLookback: 10,
          confirmByClose: true,
          equalLevelToleranceTicks: 6,
          trackInternal: true,
        },
        'm15',
      ),
    ],
  }),
  build({
    id: 'sweep-shift',
    name: 'Liquidity sweep, then structure shift',
    family: 'SMC workflow',
    description:
      'A genuine ladder: a 1-hour liquidity sweep marks the level and holds it live for five hours, and a 15-minute structure shift is the trigger. Each rung is read at its own close and projected down to the entry chart.',
    expectedFrequency: 'low',
    symbol: 'DUKAS:EURUSD',
    timeframe: 'm15',
    stop: { unit: 'pips', value: 15 },
    target: { unit: 'pips', value: 45 },
    ladder: { bias: 'h4', poi: 'h1', entry: 'm15' },
    poiWindow: '5h',
    version: 2,
    poi: [
      node(
        'sweep',
        'smc',
        'liquidity_sweep',
        {
          wickThroughTicks: 20,
          closeBackBars: 2,
          requireStructureShift: false,
        },
        'h1',
      ),
    ],
    trigger: [
      node(
        'shift',
        'smc',
        'market_structure',
        {
          swingLookback: 12,
          confirmByClose: true,
          equalLevelToleranceTicks: 8,
          trackInternal: false,
        },
        'm15',
      ),
    ],
  }),
  build({
    id: 'pin-at-swing',
    name: 'Pin bar at a swing low',
    family: 'Price action',
    description:
      'A rejection candle at a level price has already turned from. Few trades, and each one is easy to check by eye.',
    expectedFrequency: 'low',
    symbol: 'BINANCE:BTCUSDT',
    timeframe: 'h4',
    stop: { unit: 'percent', value: 1 },
    target: { unit: 'percent', value: 2.5 },
    withinBars: 4,
    poi: [
      node('swing', 'priceaction', 'swing_low', { left: 3, right: 3 }, 'h4'),
    ],
    trigger: [
      node(
        'pin',
        'priceaction',
        'pin_bar',
        { bodyRatioMilli: 300, wickRatioMilli: 600 },
        'h4',
      ),
    ],
  }),
  build({
    id: 'btc-trend-pullback',
    name: 'Bitcoin pullback to the 20-period average',
    family: 'Crypto trend following',
    description:
      'Waits for BTC to return to its 20-period average, then requires a bullish engulfing candle before entering. The stop and target are percentages, not FX pips.',
    expectedFrequency: 'medium',
    symbol: 'BINANCE:BTCUSDT',
    timeframe: 'h4',
    stop: { unit: 'percent', value: 1.5 },
    target: { unit: 'percent', value: 3 },
    withinBars: 3,
    poi: [
      node(
        'touch',
        'indicator',
        'ma_touch',
        { period: 20, toleranceTicks: 250 },
        'h4',
      ),
    ],
    trigger: [
      node(
        'engulf',
        'priceaction',
        'engulfing',
        { period: 1, enabled: true },
        'h4',
      ),
    ],
  }),
  build({
    id: 'eth-range-break',
    name: 'Ethereum range compression breakout',
    family: 'Crypto price action',
    description:
      'Looks for ETH to settle into a tight range, then enters on a decisive displacement candle. The wider percentage exits match crypto volatility.',
    expectedFrequency: 'medium',
    symbol: 'BINANCE:ETHUSDT',
    timeframe: 'h1',
    stop: { unit: 'percent', value: 1.2 },
    target: { unit: 'percent', value: 2.4 },
    withinBars: 2,
    bias: [
      node(
        'range',
        'priceaction',
        'consolidation',
        { period: 16, maxWidthTicks: 180 },
        'h1',
      ),
    ],
    trigger: [
      node(
        'push',
        'smc',
        'displacement',
        { minBodyRatioMilli: 700, minConsecutiveBars: 1, requireGap: false },
        'h1',
      ),
    ],
  }),
  build({
    id: 'btc-sweep-shift',
    name: 'Bitcoin liquidity sweep, then structure shift',
    family: 'Crypto SMC workflow',
    description:
      'A genuine ladder on BTC: a 1-hour liquidity sweep marks the level and holds it live for five hours, and a 15-minute structure shift is the trigger. Percentage exits match crypto volatility.',
    expectedFrequency: 'low',
    symbol: 'BINANCE:BTCUSDT',
    timeframe: 'h1',
    stop: { unit: 'percent', value: 1 },
    target: { unit: 'percent', value: 2.5 },
    ladder: { bias: 'h4', poi: 'h1', entry: 'm15' },
    poiWindow: '5h',
    version: 2,
    poi: [
      node(
        'sweep',
        'smc',
        'liquidity_sweep',
        {
          wickThroughTicks: 120,
          closeBackBars: 2,
          requireStructureShift: false,
        },
        'h1',
      ),
    ],
    trigger: [
      node(
        'shift',
        'smc',
        'market_structure',
        {
          swingLookback: 12,
          confirmByClose: true,
          equalLevelToleranceTicks: 40,
          trackInternal: false,
        },
        'm15',
      ),
    ],
  }),
  build({
    id: 'previous-day-high-break',
    name: 'Break of the previous day\u2019s high',
    family: 'Reference levels',
    description:
      'The most widely watched level in intraday trading. A daily high is read from an M15 chart, and the entry is the first close through it. Nothing is detected on a bar that has not finished.',
    expectedFrequency: 'medium',
    symbol: 'DUKAS:EURUSD',
    timeframe: 'm15',
    stop: { unit: 'pips', value: 15 },
    target: { unit: 'pips', value: 30 },
    ladder: { bias: 'd1', poi: 'd1', entry: 'm15' },
    trigger: [],
    level: {
      node: node('pdh', 'timeofday', 'period_high', { lookback: 0 }, 'd1'),
      timeframe: 'd1',
      source: 'close',
      relation: 'crosses_above',
    },
  }),
  build({
    id: 'opening-range-break',
    name: 'Opening-range breakout',
    family: 'Reference levels',
    description:
      'Takes the range of the first 30 minutes after the New York open and enters on the first close above it. The range is not published until it has finished forming, so the rule cannot see its own outcome.',
    expectedFrequency: 'medium',
    symbol: 'DUKAS:EURUSD',
    timeframe: 'm15',
    stop: { unit: 'pips', value: 12 },
    target: { unit: 'pips', value: 24 },
    trigger: [],
    level: {
      node: node(
        'opening-range',
        'timeofday',
        'opening_range_high',
        { startMinuteUtc: 810, rangeMinutes: 30 },
        'm15',
      ),
      source: 'close',
      relation: 'crosses_above',
    },
  }),
]

export interface TemplateCatalogue {
  templates: TemplateCard[]
  /** Set when the server could not supply the catalogue and why. */
  notice: string | null
}

/**
 * Load the catalogue, preferring the server's.
 *
 * A missing endpoint is not an error the user caused, so it produces a notice
 * rather than an empty screen. A real failure is reported as one.
 */
export async function loadTemplates(): Promise<TemplateCatalogue> {
  try {
    const response = await templateApi.listTemplates({})
    const templates = response.templates
      .map((template): TemplateCard | null => {
        const document = parseStrategy(
          JSON.stringify(fromJsonBytes<unknown>(template.strategyJson) ?? null),
        )
        if (!document) return null
        return {
          id: template.id,
          name: template.name,
          family: template.family,
          description: template.description,
          expectedFrequency: template.expectedFrequency,
          document,
          described: {
            summary:
              template.strategyDescription?.summary ??
              describeLocally(document).summary,
            rules: template.strategyDescription?.rules ?? [],
          },
          source: 'server',
        }
      })
      .filter((t): t is TemplateCard => t !== null)

    if (templates.length > 0) return { templates, notice: null }
    return {
      templates: BUNDLED_TEMPLATES,
      notice:
        'The local server returned no starting points, so these built-in ones are shown instead.',
    }
  } catch (cause) {
    if (isUnimplemented(cause)) {
      return {
        templates: BUNDLED_TEMPLATES,
        notice:
          'This local server does not serve the starting-point catalogue yet, so these built-in ones are shown instead. They are the same rules; rebuild the server to get them from it.',
      }
    }
    return {
      templates: BUNDLED_TEMPLATES,
      notice: `Could not reach the local server (${rpcErrorMessage(cause)}). Showing built-in starting points.`,
    }
  }
}

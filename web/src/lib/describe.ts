import {
  displaySymbol,
  displayTimeframe,
  expressionNodeIDs,
  isLadder,
  nodeWindowOf,
} from '@/lib/strategy'
import type {
  Distance,
  EntryOrder,
  Management,
  StrategyDoc,
  StrategyNode,
} from '@/lib/strategy'
import { displayDuration } from '@/lib/duration'

/**
 * A local rendering of a strategy in trading language.
 *
 * The server owns the real one: `StudyService.Describe` generates it from the
 * same document the engine executes, which is the only version guaranteed to
 * stay truthful. This exists so the authoring screen still says something
 * useful while a request is in flight, or when the local server is older than
 * this client — never as a replacement.
 *
 * It is deliberately marked as provisional wherever it is shown.
 */

export interface LocalRule {
  branch: string
  text: string
  enabled: boolean
}

/**
 * Read the entry order back. A resting order is the one place a study can
 * signal and not trade, so the sentence says so rather than implying every
 * signal becomes a position.
 */
function orderText(o: EntryOrder | undefined): string {
  if (!o || !o.type || o.type === 'market') return 'Fill at the next bar open'
  const where = o.node
    ? `the level from “${o.node}”`
    : o.offset
      ? `${distanceText(o.offset)} ${o.type === 'limit' ? 'better than' : 'beyond'} the signal bar's close`
      : 'its trigger level'
  return `Rest a ${o.type} order at ${where} for the next bar only, and skip the signal if it never fills`
}

/**
 * Read the trade management back in the same voice as the rest of the summary.
 * A user must be able to see the management they authored without opening the
 * document, or the tickbox and the simulated rule can drift apart unnoticed.
 */
function managementText(m: Management | undefined): string {
  if (!m) return ''
  const parts: string[] = []
  if (m.close && m.close > 0 && m.close < 1) {
    parts.push(
      `close ${Math.round(m.close * 100)}% of the position at the target and let the rest run`,
    )
    if (m.breakEven) parts.push('move the stop to break-even once that happens')
  }
  if (m.trail) parts.push(`trail the stop ${distanceText(m.trail)} behind`)
  return parts.length ? ` Then ${joinClauses(parts)}.` : ''
}

export interface LocalDescription {
  summary: string
  rules: LocalRule[]
}

function distanceText(d: Distance): string {
  switch (d.unit) {
    case 'percent':
      return `${d.value}%`
    case 'atr':
      return `${d.value}× ATR(14)`
    case 'price':
      return `${d.value} in price`
    default:
      return `${d.value} ${d.unit}`
  }
}

function param(node: StrategyNode, ...names: string[]): string {
  for (const name of names) {
    const value = node.params[name]
    if (typeof value === 'number') return String(value)
    if (typeof value === 'string' && value) return value
  }
  return '?'
}

function clock(node: StrategyNode, ...names: string[]): string {
  for (const name of names) {
    const value = node.params[name]
    if (typeof value === 'number') {
      const h = Math.floor(value / 60)
      const m = value % 60
      return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`
    }
  }
  return '?'
}

/** One primitive as a phrase. Unknown primitives are named, never hidden. */
export function describeNode(node: StrategyNode): string {
  switch (`${node.pack}/${node.type}`) {
    case 'indicator/ma_cross':
      return `the ${param(node, 'fastPeriod', 'fast_period')}-period average crosses the ${param(node, 'slowPeriod', 'slow_period')}-period average`
    case 'indicator/ma_cross_up':
      return `the ${param(node, 'fastPeriod', 'fast_period')}-period average crosses above the ${param(node, 'slowPeriod', 'slow_period')}-period average`
    case 'indicator/ma_cross_down':
      return `the ${param(node, 'fastPeriod', 'fast_period')}-period average crosses below the ${param(node, 'slowPeriod', 'slow_period')}-period average`
    case 'indicator/ma_touch':
      return `price pulls back to the ${param(node, 'period')}-period average`
    case 'priceaction/engulfing':
      return 'an engulfing candle closes'
    case 'priceaction/pin_bar':
      return 'a pin bar closes'
    case 'priceaction/inside_bar':
      return 'an inside bar closes'
    case 'priceaction/doji':
      return 'a doji closes'
    case 'priceaction/consolidation':
      return `price has held a tight range for ${param(node, 'period')} bars`
    case 'priceaction/swing_high':
      return 'a swing high forms'
    case 'priceaction/swing_low':
      return 'a swing low forms'
    case 'timeofday/session':
      return `the bar falls inside the ${clock(node, 'startMinuteUtc', 'start_minute_utc')}–${clock(node, 'endMinuteUtc', 'end_minute_utc')} UTC session`
    case 'timeofday/time_of_day_range':
      return `the time is between ${clock(node, 'startMinuteUtc', 'start_minute_utc')} and ${clock(node, 'endMinuteUtc', 'end_minute_utc')} UTC`
    case 'smc/market_structure':
      return 'market structure shifts'
    case 'smc/liquidity_sweep':
      return 'price sweeps a prior extreme and closes back inside'
    case 'smc/liquidity_pool':
      return 'price reaches a pool of resting liquidity'
    case 'smc/displacement':
      return 'a displacement candle closes'
    case 'smc/breaker':
      return 'a breaker forms'
    default:
      return `${node.pack} ${node.type.replace(/_/g, ' ')} is true`
  }
}

function joinClauses(parts: string[]): string {
  if (parts.length === 0) return ''
  if (parts.length === 1) return parts[0]
  if (parts.length === 2) return `${parts[0]} and ${parts[1]}`
  return `${parts.slice(0, -1).join(', ')}, and ${parts[parts.length - 1]}`
}

/** Every condition, in the order the engine reads the slots. */
export function conditionPhrases(
  doc: StrategyDoc,
  side: 'long' | 'short',
): string[] {
  const entryTf = doc.timeframes.entry
  const when = doc.entries[side].when
  return expressionNodeIDs(when)
    .map((id) => doc.nodes.find((node) => node.id === id))
    .filter((node): node is StrategyNode => node !== undefined)
    .map((node) => {
      let phrase = describeNode(node)
      if (node.timeframe && node.timeframe !== entryTf) {
        phrase += ` on the ${displayTimeframe(node.timeframe)} chart`
      }
      const held = nodeWindowOf(when, node.id)
      if (held) phrase += ` (still live ${displayDuration(held)} later)`
      return phrase
    })
}

export function describeLocally(doc: StrategyDoc): LocalDescription {
  const market = doc.universe.symbols[0]
    ? displaySymbol(doc.universe.symbols[0])
    : 'an unselected market'
  const entry = doc.timeframes.entry
    ? displayTimeframe(doc.timeframes.entry)
    : 'an unselected timeframe'
  const branches = (['long', 'short'] as const).map((side) => ({
    side,
    entry: doc.entries[side],
    conditions: conditionPhrases(doc, side),
  }))
  const active = branches.filter(
    ({ entry: e, conditions }) => e.enabled && conditions.length > 0,
  )

  let summary = isLadder(doc)
    ? `Trade ${market} as a ladder: form the bias on the ${
        doc.timeframes.bias
          ? displayTimeframe(doc.timeframes.bias)
          : 'an unselected'
      } chart, locate the setup on the ${
        doc.timeframes.poi
          ? displayTimeframe(doc.timeframes.poi)
          : 'an unselected'
      } chart, and trigger on the ${entry} chart. `
    : `Trade ${market} on the ${entry} chart. `
  summary +=
    active.length === 0
      ? 'No entry condition has been set yet, so nothing would ever trigger. '
      : `Enter at the next bar open when either enabled rule fires. `
  summary += `${orderText(doc.entries.order)}, place the stop ${distanceText(doc.risk.stop)} away and the target ${distanceText(doc.risk.target)} away.`
  summary += managementText(doc.risk.management)

  const rules: LocalRule[] = branches.map(({ side, entry: e, conditions }) => ({
    branch: side,
    text:
      e.name ||
      `${side === 'long' ? 'Buy' : 'Sell'} when ${joinClauses(conditions)}`,
    enabled: e.enabled,
  }))

  return { summary, rules }
}

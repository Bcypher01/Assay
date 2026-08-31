/** The executable schema implemented by honest-backtester. */
export const SCHEMA_V2 = 'assay.strategy/v2'

export const TIMEFRAMES = [
  'm1',
  'm5',
  'm15',
  'm30',
  'h1',
  'h4',
  'd1',
  'w1',
] as const
export type Timeframe = (typeof TIMEFRAMES)[number]
export type DistanceUnit = 'ticks' | 'pips' | 'percent' | 'atr' | 'price'

export interface Distance {
  unit: DistanceUnit
  value: number
}
export interface StrategyNode {
  id: string
  pack: string
  type: string
  params: Record<string, unknown>
  timeframe: string
}
/** A v2 boolean expression. The guided flow authors node references. */
export interface Expression {
  node?: string
  [key: string]: unknown
}
export interface EntryBranch {
  enabled: boolean
  name: string
  when: Expression
}
export type EntrySide = 'long' | 'short'
export interface StrategyDoc {
  schema: typeof SCHEMA_V2
  name: string
  universe: { symbols: string[] }
  timeframes: { bias: string; poi: string; entry: string }
  risk: { stop: Distance; target: Distance }
  template: { id: string; version: number }
  entries: { timing: 'next_open'; long: EntryBranch; short: EntryBranch }
  nodes: StrategyNode[]
}

function distance(value: unknown, fallback: Distance): Distance {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    return fallback
  const raw = value as Record<string, unknown>
  const unit: DistanceUnit = [
    'ticks',
    'pips',
    'percent',
    'atr',
    'price',
  ].includes(String(raw.unit))
    ? (raw.unit as DistanceUnit)
    : fallback.unit
  return {
    unit,
    value:
      typeof raw.value === 'number' && raw.value > 0
        ? raw.value
        : fallback.value,
  }
}
function node(value: unknown): StrategyNode | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  const raw = value as Record<string, unknown>
  if (typeof raw.id !== 'string' || !raw.id) return null
  return {
    id: raw.id,
    pack: typeof raw.pack === 'string' ? raw.pack : '',
    type: typeof raw.type === 'string' ? raw.type : '',
    params:
      raw.params && typeof raw.params === 'object' && !Array.isArray(raw.params)
        ? (raw.params as Record<string, unknown>)
        : {},
    timeframe: typeof raw.timeframe === 'string' ? raw.timeframe : '',
  }
}
function expression(value: unknown): Expression {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Expression)
    : {}
}
function branch(value: unknown): EntryBranch {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    return { enabled: false, name: '', when: {} }
  const raw = value as Record<string, unknown>
  return {
    enabled: raw.enabled === true,
    name: typeof raw.name === 'string' ? raw.name : '',
    when: expression(raw.when),
  }
}
function legacyWhen(value: Record<string, unknown>): Expression {
  const slots = value.slots as Record<string, unknown> | undefined
  if (!slots) return {}
  const ids = ['bias', 'poi', 'trigger'].flatMap((key) => {
    const slot = slots[key] as Record<string, unknown> | undefined
    return Array.isArray(slot?.all)
      ? slot.all.filter((id): id is string => typeof id === 'string')
      : []
  })
  return ids.length === 1
    ? { node: ids[0] }
    : ids.length
      ? { all: ids.map((id) => ({ node: id })) }
      : {}
}

export function emptyStrategy(): StrategyDoc {
  return withStarterEntry({
    schema: SCHEMA_V2,
    name: 'New study',
    universe: { symbols: [] },
    timeframes: { bias: '', poi: '', entry: '' },
    risk: {
      stop: { unit: 'pips', value: 20 },
      target: { unit: 'pips', value: 40 },
    },
    template: { id: 'guided-crossover', version: 1 },
    entries: {
      timing: 'next_open',
      long: { enabled: false, name: '', when: {} },
      short: { enabled: false, name: '', when: {} },
    },
    nodes: [],
  })
}

/** Parse and normalise legacy documents into v2 before they are written again. */
export function parseStrategy(text: string): StrategyDoc | null {
  let value: Record<string, unknown>
  try {
    const raw: unknown = JSON.parse(text)
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null
    value = raw as Record<string, unknown>
  } catch {
    return null
  }
  const base = emptyStrategy()
  const universe =
    value.universe && typeof value.universe === 'object'
      ? (value.universe as Record<string, unknown>)
      : {}
  const timeframes =
    value.timeframes && typeof value.timeframes === 'object'
      ? (value.timeframes as Record<string, unknown>)
      : {}
  const risk =
    value.risk && typeof value.risk === 'object'
      ? (value.risk as Record<string, unknown>)
      : {}
  const template =
    value.template && typeof value.template === 'object'
      ? (value.template as Record<string, unknown>)
      : {}
  const entries =
    value.entries && typeof value.entries === 'object'
      ? (value.entries as Record<string, unknown>)
      : null
  const when = legacyWhen(value)
  const direction = value.direction
  const legacyLong = direction !== 'short'
  const legacyShort = direction === 'short' || direction === 'both'
  const document: StrategyDoc = {
    schema: SCHEMA_V2,
    name: typeof value.name === 'string' ? value.name : base.name,
    universe: {
      symbols: Array.isArray(universe.symbols)
        ? universe.symbols.filter((s): s is string => typeof s === 'string')
        : [],
    },
    timeframes: {
      bias: typeof timeframes.bias === 'string' ? timeframes.bias : '',
      poi: typeof timeframes.poi === 'string' ? timeframes.poi : '',
      entry: typeof timeframes.entry === 'string' ? timeframes.entry : '',
    },
    risk: {
      stop: distance(risk.stop, base.risk.stop),
      target: distance(risk.target, base.risk.target),
    },
    template: {
      id:
        typeof template.id === 'string' && template.id
          ? template.id
          : 'legacy-v1',
      version:
        typeof template.version === 'number' && template.version > 0
          ? template.version
          : 1,
    },
    entries: entries
      ? {
          timing: 'next_open',
          long: branch(entries.long),
          short: branch(entries.short),
        }
      : {
          timing: 'next_open',
          long: {
            enabled: legacyLong && Object.keys(when).length > 0,
            name: 'Legacy long entry',
            when,
          },
          short: {
            enabled: legacyShort && Object.keys(when).length > 0,
            name: 'Legacy short entry',
            when,
          },
        },
    nodes: Array.isArray(value.nodes)
      ? value.nodes.map(node).filter((n): n is StrategyNode => n !== null)
      : [],
  }
  // Legacy empty builders were never executable. Upgrade them to the guided
  // starter rather than carrying a guaranteed validation failure forward.
  return !entries && Object.keys(when).length === 0
    ? withStarterEntry(document)
    : document
}
export function withNode(
  doc: StrategyDoc,
  id: string,
  patch: Partial<StrategyNode>,
): StrategyDoc {
  return {
    ...doc,
    nodes: doc.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n)),
  }
}
export function withNodeParams(
  doc: StrategyDoc,
  id: string,
  params: Record<string, unknown>,
): StrategyDoc {
  const current = doc.nodes.find((n) => n.id === id)
  return current
    ? withNode(doc, id, { params: { ...current.params, ...params } })
    : doc
}
export function withMarket(
  doc: StrategyDoc,
  symbol: string,
  timeframe: string,
): StrategyDoc {
  return {
    ...doc,
    universe: { symbols: symbol ? [symbol] : [] },
    timeframes: { bias: timeframe, poi: timeframe, entry: timeframe },
    nodes: doc.nodes.map((n) => ({ ...n, timeframe })),
  }
}
export function expressionNodeIDs(value: Expression): string[] {
  const ids = new Set<string>()
  const visit = (item: unknown) => {
    if (!item || typeof item !== 'object' || Array.isArray(item)) return
    const x = item as Record<string, unknown>
    if (typeof x.node === 'string') ids.add(x.node)
    for (const value of Object.values(x)) {
      if (Array.isArray(value)) value.forEach(visit)
      else visit(value)
    }
  }
  visit(value)
  return [...ids]
}
export function hasEntry(doc: StrategyDoc): boolean {
  return [doc.entries.long, doc.entries.short].some(
    (entry) => entry.enabled && expressionNodeIDs(entry.when).length > 0,
  )
}

/**
 * The simulator executes one position side per study. Keep that constraint
 * visible in the document itself, rather than leaving a user with two enabled
 * branches that validation must reject. Switching direction intentionally
 * keeps the entry condition unchanged: it changes the position taken when that
 * condition fires, not the condition's market meaning.
 */
export function withEntrySide(doc: StrategyDoc, side: EntrySide): StrategyDoc {
  const other: EntrySide = side === 'long' ? 'short' : 'long'
  const current = doc.entries[side]
  const fallback = doc.entries[other]
  const source = expressionNodeIDs(current.when).length > 0 ? current : fallback
  // Keep the backend-provided condition label (for example, “Bullish 20/50
  // crossover”). The side is already shown by the direction control and entry
  // heading; prepending the strategy title produced “Go long when long …”.
  const name = source.name || (side === 'long' ? 'Long entry' : 'Short entry')

  return {
    ...doc,
    entries: {
      timing: doc.entries.timing,
      [side]: { ...source, enabled: true, name },
      [other]: { enabled: false, name: '', when: {} },
    },
  }
}

/**
 * Keep the server-owned v2 strategy intact after enforcing the UI's one-side
 * run choice. The live Assay server validates and executes v2 branches; an
 * earlier v1 adapter discarded their expression structure.
 */
export function toEngineStrategy(doc: StrategyDoc): Record<string, unknown> {
  const active = (['long', 'short'] as const).filter((side) => {
    const entry = doc.entries[side]
    return entry.enabled && expressionNodeIDs(entry.when).length > 0
  })
  if (active.length !== 1) {
    throw new Error(
      active.length === 0
        ? 'Add one long or short entry condition before validating.'
        : 'Run long and short entries as separate studies; the current engine executes one side at a time.',
    )
  }
  return doc as unknown as Record<string, unknown>
}
/** Add an executable default without enabling both sides of a one-side engine. */
export function withStarterEntry(doc: StrategyDoc): StrategyDoc {
  if (hasEntry(doc)) return doc
  const ids = new Set(doc.nodes.map((n) => n.id))
  const unique = (base: string) => {
    let id = base
    let n = 2
    while (ids.has(id)) id = `${base}-${n++}`
    ids.add(id)
    return id
  }
  const long = unique('bullish-cross')
  return {
    ...doc,
    nodes: [
      ...doc.nodes,
      {
        id: long,
        pack: 'indicator',
        type: 'ma_cross_bullish',
        params: { fastPeriod: 20, slowPeriod: 50 },
        timeframe: doc.timeframes.entry,
      },
    ],
    entries: {
      timing: 'next_open',
      long: {
        enabled: true,
        name: 'Bullish 20/50 crossover',
        when: { node: long },
      },
      short: { enabled: false, name: '', when: {} },
    },
  }
}
export function symbolOf(doc: StrategyDoc): string {
  return doc.universe.symbols[0] ?? ''
}
export function displaySymbol(symbol: string): string {
  const bare = symbol.includes(':')
    ? symbol.slice(symbol.indexOf(':') + 1)
    : symbol
  return bare.length === 6 && bare === bare.toUpperCase()
    ? `${bare.slice(0, 3)}/${bare.slice(3)}`
    : bare || symbol
}
const TIMEFRAME_LABELS: Record<string, string> = {
  m1: '1-minute',
  m5: '5-minute',
  m15: '15-minute',
  m30: '30-minute',
  h1: '1-hour',
  h4: '4-hour',
  d1: 'daily',
  w1: 'weekly',
}
export function displayTimeframe(tf: string): string {
  return TIMEFRAME_LABELS[tf] ?? tf
}

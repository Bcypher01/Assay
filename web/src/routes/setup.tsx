import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  ChevronLeft,
  Loader2,
  MousePointerClick,
  Play,
  Save,
} from 'lucide-react'
import { SetupChart, drawingStorageKeyFor } from '@/components/setup-chart'
import type { SetupLines } from '@/components/setup-chart'
import type { ChartDrawing } from '@/lib/chart-drawings'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  sortTimeframes,
  supportedTimeframesForInstrument,
  timeframeMinutes,
} from '@/lib/constraints'
import { dataApi } from '@/lib/rpc'
import type {
  Drawing,
  SetupChart as ChartData,
  SetupKind,
  SetupPreview,
  SetupMatch,
  SetupSide,
  PrecedentBar,
  SetupBar,
} from '@/lib/setup'
import {
  SETUP_KINDS,
  fetchSetupChart,
  friendlySetupError,
  formatTicks,
  inspectBars,
  isSetupUnavailable,
  previewSetup,
  riskReward,
  runSetup,
  validateDrawing,
} from '@/lib/setup'
import { formatMs } from '@/lib/codec'
import { displaySymbol, displayTimeframe } from '@/lib/strategy'
import { session, useSession } from '@/lib/store'

export const Route = createFileRoute('/setup')({
  component: SetupTestScreen,
  // Owns the whole viewport: no run-log spine, no page column.
  staticData: { fullscreen: true },
})

/**
 * The Setup Test.
 *
 * A trader draws one example of the thing they trade, confirms in words what
 * it means, and Assay repeats *that rule* across history. The drawing is never
 * repeated: the absolute EUR/USD price the user clicked on is an illustration,
 * and the study runs on the structural rule the drawing was labelled with.
 *
 * The screen is deliberately gated. Nothing can be saved until the confirmed
 * rule actually fires on the example candle the user chose, because a rule that
 * misses its own example is a rule the user has not described yet — and a
 * backtest of it would be an answer to a question nobody asked.
 */

const HISTORY_YEARS = [1, 3, 5, 10] as const
const RISK_CHOICES: Array<[string, string]> = [
  ['0.0025', '0.25% of the account'],
  ['0.005', '0.5% of the account'],
  ['0.01', '1% of the account'],
  ['0.02', '2% of the account'],
]
/**
 * The server returns the most recent N bars of the window, untouched — it
 * truncates rather than downsampling, so every candle on screen is a real one.
 * Asking for more than the chart opens on gives room to zoom out into actual
 * history instead of into an empty axis.
 */
const MAX_BARS = 20_000
const COVERAGE_TOLERANCE_MS = 7 * 86_400_000
/**
 * How much of the requested span the local cache must already hold before its
 * bars are shown as-is. A source that has not published the latest days (or is
 * rate-limiting us) should not turn opening the chart into a two-minute wait:
 * when almost the whole window is already prepared, the honest answer is to
 * show it and say where it ends, not to fetch the tail and time out.
 */
const CACHE_ENOUGH_RATIO = 0.9
interface InstrumentRow {
  symbol: string
  timeframes: Array<string>
}

interface MarketResult {
  symbol: string
  stage: 'queued' | 'preparing' | 'evaluating' | 'done' | 'error'
  trades: number
  wins: number
  losses: number
  expectancy: number
  warning?: string
}

interface RuleState {
  lookbackBars: number
  retestBars: number
  toleranceTicks: number
  expiryBars: number
  riskPerTrade: number
  useBody: boolean
  confirmByClose: boolean
  minSizeTicks: number
  mitigationRule: 'first_touch' | 'half_fill' | 'full_fill'
  similarityMilli: number
}

const DEFAULT_RULE: RuleState = {
  lookbackBars: 20,
  retestBars: 10,
  toleranceTicks: 5,
  expiryBars: 50,
  riskPerTrade: 0.005,
  useBody: false,
  confirmByClose: true,
  minSizeTicks: 5,
  mitigationRule: 'first_touch',
  similarityMilli: 900,
}

function windowFor(years: number) {
  // Never ask the loader to prepare future candles. The old +1 day window
  // made the current partition look permanently incomplete, so every reload
  // and every market comparison downloaded it again.
  const to = Date.now()
  const from = new Date(to)
  from.setUTCFullYear(from.getUTCFullYear() - years)
  return { fromMs: from.getTime(), toMs: to }
}

/** Map a viewport from one timeframe's bars to another, preserving the visible time range. */
function mapViewportToTimeframe(
  viewport: { offset: number; count: number },
  sourceBars: Array<SetupBar>,
  targetBars: Array<SetupBar>,
): { offset: number; count: number } | null {
  if (sourceBars.length === 0 || targetBars.length === 0) return null
  const sourceStart = sourceBars[viewport.offset]?.timeMs
  const sourceEnd =
    sourceBars[
      Math.min(viewport.offset + viewport.count - 1, sourceBars.length - 1)
    ]?.timeMs
  if (!sourceStart || !sourceEnd) return null
  // Find corresponding indices in target bars
  let targetStartIndex = targetBars.findIndex(
    (bar) => bar.timeMs >= sourceStart,
  )
  let targetEndIndex = targetBars.findIndex((bar) => bar.timeMs > sourceEnd)
  if (targetStartIndex < 0) targetStartIndex = 0
  if (targetEndIndex < 0) targetEndIndex = targetBars.length - 1
  const count = Math.max(1, targetEndIndex - targetStartIndex + 1)
  // Clamp to target bounds
  const offset = Math.max(
    0,
    Math.min(targetBars.length - count, targetStartIndex),
  )
  return { offset, count: Math.min(count, targetBars.length - offset) }
}

function normalizedPrecedent(
  chart: ChartData,
  fromTimeMs: number,
  anchorIndex: number,
): Array<PrecedentBar> {
  const fromIndex = chart.bars.findIndex((bar) => bar.timeMs >= fromTimeMs)
  if (fromIndex < 0 || anchorIndex < fromIndex) return []
  const source = chart.bars.slice(fromIndex, anchorIndex + 1)
  if (source.length < 3) return []
  const bars =
    source.length <= 64
      ? source
      : Array.from(
          { length: 64 },
          (_, index) => source[Math.round((index * (source.length - 1)) / 63)],
        )
  const low = Math.min(...bars.map((bar) => bar.low))
  const high = Math.max(...bars.map((bar) => bar.high))
  const span = high - low
  if (span <= 0) return []
  const normalize = (price: number) => Math.round(((price - low) * 1000) / span)
  return bars.map((bar) => ({
    openMilli: normalize(bar.open),
    highMilli: normalize(bar.high),
    lowMilli: normalize(bar.low),
    closeMilli: normalize(bar.close),
  }))
}

function coversRequestedWindow(
  chart: ChartData,
  fromMs: number,
  toMs: number,
): boolean {
  // Weekends, holidays and the still-open candle legitimately leave a small
  // gap at either edge. A full maxBars response is also intentionally
  // truncated by the chart endpoint and therefore valid.
  return (
    chart.bars.length === MAX_BARS ||
    (chart.fromMs <= fromMs + COVERAGE_TOLERANCE_MS &&
      chart.toMs >= toMs - COVERAGE_TOLERANCE_MS)
  )
}

/** True when the cached bars are a fair rendering of the requested window. */
function cacheCoversEnough(
  chart: ChartData,
  fromMs: number,
  toMs: number,
): boolean {
  if (chart.bars.length === 0) return false
  if (coversRequestedWindow(chart, fromMs, toMs)) return true
  // The cache still has to reach the start of the window; a chart that begins
  // years late is a genuine miss, not a short tail.
  if (chart.fromMs > fromMs + COVERAGE_TOLERANCE_MS) return false
  const span = Math.max(1, toMs - fromMs)
  const missingTail = Math.max(0, toMs - chart.toMs)
  return missingTail <= span * (1 - CACHE_ENOUGH_RATIO)
}

async function loadSetupChart(
  request: Omit<Parameters<typeof fetchSetupChart>[0], 'prepare'>,
): Promise<ChartData> {
  try {
    const cached = await fetchSetupChart({ ...request, prepare: false })
    if (cacheCoversEnough(cached, request.fromMs, request.toMs)) {
      return cached
    }
  } catch {
    // A cache miss is expected on the first request. Preparation below reports
    // the actionable error if the source cannot provide the data.
  }
  return fetchSetupChart({ ...request, prepare: true })
}

/** True when two setups are pixel-for-pixel the same, so React can keep the
 *  previous object and skip a render. */
function sameLines(a: SetupLines | null, b: SetupLines | null): boolean {
  if (a === b) return true
  if (!a || !b) return false
  return (
    a.entry === b.entry &&
    a.stop === b.stop &&
    a.target === b.target &&
    a.levelLow === b.levelLow &&
    a.levelHigh === b.levelHigh
  )
}

/** Sensible opening lines for a freshly picked candle. */
function linesForBar(
  bar: { open: number; high: number; low: number; close: number },
  pipTicks: number,
  side: SetupSide,
): SetupLines {
  const pip = Math.max(1, pipTicks)
  const span = 20 * pip
  const base = side === 'long' ? bar.low : bar.high
  const other = side === 'long' ? bar.low + pip : bar.high - pip
  return {
    entry: bar.close,
    levelLow: Math.min(base, other),
    levelHigh: Math.max(base, other),
    stop: side === 'long' ? bar.close - span : bar.close + span,
    target: side === 'long' ? bar.close + span * 2 : bar.close - span * 2,
  }
}

function SetupTestScreen() {
  const navigate = useNavigate()
  const sessionState = useSession()

  const [instruments, setInstruments] = useState<Array<InstrumentRow>>([])
  const [symbol, setSymbol] = useState('DUKAS:EURUSD')
  const [timeframe, setTimeframe] = useState('d1')
  // Empty means the entry is the same rung as the pattern (single-timeframe).
  const [entryTimeframe, setEntryTimeframe] = useState('')
  const [years, setYears] = useState(3)

  const [chart, setChart] = useState<ChartData | null>(null)
  const [entryChart, setEntryChart] = useState<ChartData | null>(null)
  const [preparing, setPreparing] = useState(false)
  const [scaledDown, setScaledDown] = useState(false)
  const [scaleOpen, setScaleOpen] = useState(false)
  const [status, setStatus] = useState<{
    tone: 'idle' | 'busy' | 'good' | 'bad'
    message: string
  }>({ tone: 'idle', message: 'Choose a market, then prepare the chart.' })

  // Cache prepared charts per symbol+timeframe+years to avoid re-preparing on scale-down
  const chartCacheRef = useRef<Map<string, ChartData>>(new Map())
  const chartCacheKey = (sym: string, tf: string, yrs: number) =>
    `${sym}:${tf}:${yrs}`
  // Bumped every time a load begins. If a newer load starts while an earlier
  // one is still fetching, the older response must not own the UI afterwards.
  const loadSeqRef = useRef(0)
  // The (symbol, timeframe, years) triple the on-screen chart was loaded for.
  // Auto-prepare on change — but only on change, never on an unrelated
  // re-render or a scale-down rung switch.
  const preparedKeyRef = useRef<string | null>(null)

  const [setupMode, setSetupMode] = useState<'drawing' | 'template'>('drawing')
  const [kind, setKind] = useState<SetupKind>('drawn_precedent')
  const [side, setSide] = useState<SetupSide>('long')
  const [zonePlaced, setZonePlaced] = useState(false)
  const [entryPlaced, setEntryPlaced] = useState(false)
  const [precedentFromTimeMs, setPrecedentFromTimeMs] = useState<number | null>(
    null,
  )
  const [anchorIndex, setAnchorIndex] = useState<number | null>(null)
  const [lines, setLines] = useState<SetupLines | null>(null)
  const [rule, setRule] = useState<RuleState>(DEFAULT_RULE)

  const [preview, setPreview] = useState<SetupPreview | null>(null)
  const [focusedMatch, setFocusedMatch] = useState<SetupMatch | null>(null)
  const [saving, setSaving] = useState(false)
  const [unavailable, setUnavailable] = useState<string | null>(null)
  const [visualSource, setVisualSource] = useState<string | null>(null)

  // Viewport persistence for HTF↔LTF switching
  const [patternViewport, setPatternViewport] = useState<{
    offset: number
    count: number
  } | null>(null)
  const [entryViewport, setEntryViewport] = useState<{
    offset: number
    count: number
  } | null>(null)

  const [comparisonSymbols, setComparisonSymbols] = useState<Array<string>>([
    'DUKAS:EURUSD',
  ])
  const [marketResults, setMarketResults] = useState<Array<MarketResult>>([])
  const [comparing, setComparing] = useState(false)
  const chartRegionRef = useRef<HTMLDivElement>(null)
  const resultsRef = useRef<HTMLDivElement>(null)

  const revealResults = useCallback(() => {
    // Results are mounted by the same state update that starts the test. Two
    // frames let React commit them before we move the workspace viewport.
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        resultsRef.current?.scrollIntoView({
          behavior: 'smooth',
          block: 'start',
        })
        resultsRef.current?.focus({ preventScroll: true })
      })
    })
  }, [])

  const revealChart = useCallback(() => {
    chartRegionRef.current?.scrollIntoView({
      behavior: 'smooth',
      block: 'start',
    })
  }, [])

  const timeframes = useMemo(() => {
    const row = instruments.find((item) => item.symbol === symbol)
    return row?.timeframes ?? ['m15', 'h1', 'h4', 'd1']
  }, [instruments, symbol])

  const comparableMarkets = useMemo(
    () => instruments.filter((item) => item.timeframes.includes(timeframe)),
    [instruments, timeframe],
  )

  useEffect(() => {
    let cancelled = false
    void dataApi
      .listInstruments({})
      .then((response) => {
        if (cancelled) return
        const rows = response.instruments
          .map((item) => ({
            symbol: item.symbol,
            timeframes: sortTimeframes(
              supportedTimeframesForInstrument(item.symbol, item.timeframes),
            ),
          }))
          .filter((item) => item.timeframes.length > 0)
        setInstruments(rows)
        if (rows.length > 0 && !rows.some((item) => item.symbol === symbol)) {
          setSymbol(rows[0].symbol)
        }
      })
      .catch(() => {
        // The instrument catalogue is a convenience here. The Setup Test can
        // still run against the default market if the RPC is unavailable.
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (timeframes.length > 0 && !timeframes.includes(timeframe)) {
      setTimeframe(timeframes.includes('d1') ? 'd1' : timeframes[0])
    }
  }, [timeframes, timeframe])

  useEffect(() => {
    setComparisonSymbols((current) =>
      current.includes(symbol) ? current : [symbol, ...current],
    )
  }, [symbol])

  /** Any change to the drawing or the rule invalidates a previous evaluation. */
  const invalidate = useCallback(() => {
    setPreview(null)
    setFocusedMatch(null)
  }, [])

  const selectAnchor = useCallback(
    (index: number) => {
      if (!chart) return
      invalidate()
      setAnchorIndex(index)
      setLines(null)
    },
    [chart, invalidate],
  )

  const adoptVisualDrawing = useCallback(
    (visual: ChartDrawing | null) => {
      if (!visual || !chart) return
      // The drawing may have been placed on another timeframe's candles. Adopt
      // it by the same fuzzy rule the viewport mapper and the chart renderer
      // use — the first bar at or after the marker's time — so a setup drawn on
      // daily can be adopted from the H4 surface it now renders on.
      const index = chart.bars.findIndex(
        (bar) => bar.timeMs >= visual.from.timeMs,
      )
      if (index < 0) return
      if (visual.type === 'zone') {
        const low = Math.min(visual.from.price, visual.to.price)
        const high = Math.max(visual.from.price, visual.to.price)
        const startTime = Math.min(visual.from.timeMs, visual.to.timeMs)
        // Selecting the shape that is already adopted must not throw away the
        // results the user is reading. Only a real change invalidates them.
        const unchanged =
          kind === 'drawn_precedent' &&
          zonePlaced &&
          precedentFromTimeMs === startTime &&
          lines?.levelLow === low &&
          lines?.levelHigh === high
        if (unchanged) return
        invalidate()
        setKind('drawn_precedent')
        setSetupMode('drawing')
        setZonePlaced(true)
        setPrecedentFromTimeMs(Math.min(visual.from.timeMs, visual.to.timeMs))
        setVisualSource(
          `${visual.text || 'Setup area'} defines the start of this precedent`,
        )
        setLines((current) => {
          const base =
            current ?? linesForBar(chart.bars[index], chart.pipTicks, 'long')
          const next = { ...base, levelLow: low, levelHigh: high }
          return sameLines(current, next) ? current : next
        })
        // The zone describes the pattern. It must never silently move the
        // entry candle after the trader has placed an explicit marker.
        setAnchorIndex((current) => (entryPlaced ? current : index))
        return
      }
      if (visual.type === 'long' || visual.type === 'short') {
        const nextSide = visual.type
        const entry = visual.from.price
        const unchanged =
          side === nextSide &&
          entryPlaced &&
          anchorIndex === index &&
          lines?.entry === entry
        if (unchanged) return
        invalidate()
        setSide(nextSide)
        setSetupMode('drawing')
        setEntryPlaced(true)
        setAnchorIndex(index)
        setVisualSource(
          `${nextSide === 'long' ? 'Long' : 'Short'} entry translated from the selected marker`,
        )
        setLines((current) => {
          const base =
            current ?? linesForBar(chart.bars[index], chart.pipTicks, nextSide)
          const risk = Math.max(1, Math.abs(base.entry - base.stop))
          const reward = Math.max(1, Math.abs(base.target - base.entry))
          const next = {
            ...base,
            entry: visual.from.price,
            stop:
              nextSide === 'long'
                ? visual.from.price - risk
                : visual.from.price + risk,
            target:
              nextSide === 'long'
                ? visual.from.price + reward
                : visual.from.price - reward,
          }
          return sameLines(current, next) ? current : next
        })
      }
    },
    [
      chart,
      entryPlaced,
      invalidate,
      kind,
      zonePlaced,
      precedentFromTimeMs,
      lines,
      side,
      anchorIndex,
    ],
  )

  const prepare = useCallback(async () => {
    const range = windowFor(years)
    const priorChart = chart
    const priorAnchorIndex = anchorIndex
    const priorLines = lines
    const seq = ++loadSeqRef.current
    setPreparing(true)
    setUnavailable(null)
    setStatus({ tone: 'busy', message: 'Preparing versioned bars…' })
    try {
      const cacheKey = chartCacheKey(symbol, timeframe, years)
      let next = chartCacheRef.current.get(cacheKey)
      if (!next) {
        next = await loadSetupChart({
          symbol,
          timeframe,
          fromMs: range.fromMs,
          toMs: range.toMs,
          maxBars: MAX_BARS,
        })
        chartCacheRef.current.set(cacheKey, next)
      }
      // An entry rung chosen via scale-down needs its own bars prepared: the
      // pattern is detected on the drawing timeframe but the simulator fills on
      // the entry timeframe. A reload keeps the rung we are already on.
      let nextEntry: ChartData | null = null
      if (entryTimeframe && entryTimeframe !== timeframe) {
        const entryCacheKey = chartCacheKey(symbol, entryTimeframe, years)
        const cached = chartCacheRef.current.get(entryCacheKey)
        nextEntry = cached ?? null
        if (!nextEntry) {
          nextEntry = await loadSetupChart({
            symbol,
            timeframe: entryTimeframe,
            fromMs: range.fromMs,
            toMs: range.toMs,
            maxBars: MAX_BARS,
          })
          chartCacheRef.current.set(entryCacheKey, nextEntry)
        }
      }
      // A newer load started while this one was fetching; it owns the UI now.
      if (seq !== loadSeqRef.current) return
      setChart(next)
      setEntryChart(nextEntry)
      setPreview(null)
      setScaledDown(
        entryTimeframe !== '' &&
          entryTimeframe !== timeframe &&
          nextEntry !== null,
      )
      // Carry an existing setup across a timeframe or history change instead of
      // dropping it. The anchor travels by time — the first closed bar at or
      // after the previous anchor's open — and the entry/stop/target plus the
      // precedent survive as-is, being absolute ticks and timestamps with no
      // resolution. The timeframe switch no longer blanks the chart, so `chart`
      // still describes the series the setup was defined on; a fresh market
      // (symbol change nulls it beforehand) starts from scratch.
      let anchor = Math.max(0, next.bars.length - 2)
      let carryLines = false
      const priorAnchorTime =
        priorChart && priorAnchorIndex !== null
          ? priorChart.bars[priorAnchorIndex]?.timeMs
          : undefined
      if (
        priorAnchorTime !== undefined &&
        next.bars.length > 0 &&
        priorAnchorTime >= next.bars[0].timeMs
      ) {
        const remapped = next.bars.findIndex(
          (bar) => bar.timeMs >= priorAnchorTime,
        )
        if (remapped >= 0) {
          anchor = remapped
          carryLines = true
        }
      }
      const anchorBar = next.bars.at(anchor)
      setAnchorIndex(anchorBar ? anchor : null)
      setLines(carryLines ? priorLines : null)
      // Say where a short tail ends: the window is the one the user asked for,
      // but the bars are the ones that exist, and the label must not imply
      // coverage the chart does not have.
      const endsEarly = next.toMs < range.toMs - COVERAGE_TOLERANCE_MS
      setStatus({
        tone: 'good',
        message: `${next.bars.length} candles${endsEarly ? ` · through ${formatMs(next.toMs)}` : ''} · snapshot ${next.snapshot.slice(0, 10)}…`,
      })
    } catch (error) {
      if (seq !== loadSeqRef.current) return
      console.error('[Setup Test] Chart preparation failed', error)
      if (isSetupUnavailable(error)) {
        setUnavailable(friendlySetupError(error, 'prepare'))
        setStatus({ tone: 'bad', message: 'Setup Test not available' })
      } else {
        setStatus({
          tone: 'bad',
          message: friendlySetupError(error, 'prepare'),
        })
      }
    } finally {
      if (seq === loadSeqRef.current) setPreparing(false)
    }
  }, [
    symbol,
    timeframe,
    entryTimeframe,
    years,
    side,
    chart,
    anchorIndex,
    lines,
  ])

  // Auto-load the chart for the (symbol, timeframe, years) actually selected.
  // The key guard means unrelated re-renders and scale-down rung changes never
  // trigger a reload — and the availability check lets the timeframe-correction
  // effect (above) settle before the corrected choice is fetched.
  // We do NOT include `prepare` in deps to avoid the circular dependency that
  // caused the "reload button does nothing" issue. The sequence tracking inside
  // prepare() handles concurrent load protection, and the preparedKeyRef check
  // ensures we only load once per (symbol, timeframe, years) triple.
  useEffect(() => {
    if (!symbol || !timeframe) return
    if (timeframes.length > 0 && !timeframes.includes(timeframe)) return
    const key = `${symbol}|${timeframe}|${years}`
    if (preparedKeyRef.current === key) return
    preparedKeyRef.current = key
    void prepare()
  }, [symbol, timeframe, years, timeframes])

  /** Scale the chart down to an entry rung, preparing its bars on demand. */
  const scaleTo = useCallback(
    async (rung: string) => {
      if (rung === timeframe) {
        setScaledDown(false)
        return
      }
      const cacheKey = chartCacheKey(symbol, rung, years)
      const cached = chartCacheRef.current.get(cacheKey)
      if (cached) {
        // Map current pattern viewport to entry timeframe
        let mappedViewport: { offset: number; count: number } | null = null
        if (patternViewport && chart) {
          mappedViewport = mapViewportToTimeframe(
            patternViewport,
            chart.bars,
            cached.bars,
          )
        }
        if (mappedViewport) setEntryViewport(mappedViewport)
        setEntryChart(cached)
        setEntryTimeframe(rung)
        setScaledDown(true)
        setPreview(null)
        setFocusedMatch(null)
        setStatus({
          tone: 'good',
          message: `${cached.bars.length} candles · snapshot ${cached.snapshot.slice(0, 10)}…`,
        })
        return
      }
      if (entryTimeframe === rung && entryChart) {
        setScaledDown(true)
        return
      }
      const range = windowFor(years)
      setPreparing(true)
      setUnavailable(null)
      setStatus({
        tone: 'busy',
        message: `Preparing ${displayTimeframe(rung)} bars…`,
      })
      try {
        const next = await loadSetupChart({
          symbol,
          timeframe: rung,
          fromMs: range.fromMs,
          toMs: range.toMs,
          maxBars: MAX_BARS,
        })
        chartCacheRef.current.set(cacheKey, next)
        // Map current pattern viewport to entry timeframe
        let mappedViewport: { offset: number; count: number } | null = null
        if (patternViewport && chart) {
          mappedViewport = mapViewportToTimeframe(
            patternViewport,
            chart.bars,
            next.bars,
          )
        }
        if (mappedViewport) setEntryViewport(mappedViewport)
        setEntryChart(next)
        setEntryTimeframe(rung)
        setScaledDown(true)
        setPreview(null)
        setFocusedMatch(null)
        setStatus({
          tone: 'good',
          message: `${next.bars.length} candles · snapshot ${next.snapshot.slice(0, 10)}…`,
        })
      } catch (error) {
        console.error('[Setup Test] Entry rung preparation failed', error)
        if (isSetupUnavailable(error)) {
          setUnavailable(friendlySetupError(error, 'prepare'))
          setStatus({ tone: 'bad', message: 'Setup Test not available' })
        } else {
          setStatus({
            tone: 'bad',
            message: friendlySetupError(error, 'prepare'),
          })
        }
      } finally {
        setPreparing(false)
      }
    },
    [
      symbol,
      timeframe,
      entryTimeframe,
      entryChart,
      years,
      patternViewport,
      chart,
    ],
  )

  // Rungs strictly finer than the pattern: these are what a scale-down can
  // show. The user declares the entry rung by scaling to it, not by picking it
  // in a top-nav list they never look at.
  const finerTimeframes = useMemo(() => {
    const pattern = timeframeMinutes(timeframe)
    if (pattern === null) return []
    return timeframes.filter((item) => {
      const m = timeframeMinutes(item)
      return m !== null && m < pattern
    })
  }, [timeframes, timeframe])

  const drawing: Drawing | null = useMemo(() => {
    if (!chart || anchorIndex === null || !lines) return null
    if (setupMode === 'drawing' && (!zonePlaced || !entryPlaced)) return null
    const bar = chart.bars.at(anchorIndex)
    if (!bar) return null
    const precedent =
      kind === 'drawn_precedent' && precedentFromTimeMs !== null
        ? normalizedPrecedent(chart, precedentFromTimeMs, anchorIndex)
        : []
    return {
      kind,
      side,
      symbol: chart.symbol,
      timeframe: chart.timeframe,
      entryTimeframe: entryTimeframe || undefined,
      anchorTimeMs: bar.timeMs,
      levelLow: Math.round(lines.levelLow),
      levelHigh: Math.round(lines.levelHigh),
      entry: Math.round(lines.entry),
      stop: Math.round(lines.stop),
      target: Math.round(lines.target),
      lookbackBars: rule.lookbackBars,
      retestBars: rule.retestBars,
      toleranceTicks: rule.toleranceTicks,
      expiryBars: rule.expiryBars,
      // Fixed by the semantic pack: a displacement candle is one whose body is
      // at least 60% of its range. It is not a knob because a user cannot
      // calibrate it without overfitting it.
      displacementBodyMilli: 600,
      useBody: rule.useBody,
      confirmByClose: rule.confirmByClose,
      minSizeTicks: rule.minSizeTicks,
      mitigationRule: rule.mitigationRule,
      riskUnit:
        kind === 'fair_value_gap_retest' || kind === 'drawn_precedent'
          ? 'percent'
          : 'ticks',
      riskPerTrade: rule.riskPerTrade,
      precedent,
      similarityMilli: rule.similarityMilli,
    }
  }, [
    chart,
    anchorIndex,
    lines,
    kind,
    side,
    rule,
    setupMode,
    zonePlaced,
    entryPlaced,
    precedentFromTimeMs,
    entryTimeframe,
  ])

  // Structural checks on the bars that actually came back. A study is worth
  // what its data is worth, so a series that cannot support the question gets
  // said out loud on the screen rather than discovered in the result.
  const diagnostics = useMemo(() => inspectBars(chart), [chart])

  const errors = useMemo(
    () => (drawing ? validateDrawing(drawing) : {}),
    [drawing],
  )
  const errorList = Object.values(errors)
  const rr = drawing ? riskReward(drawing) : null

  // Scale-down shows the entry rung's own candles so the entry is picked from
  // the precedent area's fine structure. All prices are absolute ticks, so the
  // drawn lines and the storage of the drawings themselves are unchanged; only
  // the underlying candle series (and the anchor, which lives on the pattern
  // timeframe) need remapping.
  const displayChart = scaledDown && entryChart ? entryChart : chart
  const displayAnchorIndex = useMemo(() => {
    if (!scaledDown || !entryChart || !chart || anchorIndex === null)
      return anchorIndex
    const patternTime = chart.bars[anchorIndex]?.timeMs
    if (patternTime === undefined) return null
    // The finer series is bucketed on the same open-time clock but may open a
    // session a bar earlier, carry a DST duplicate, or start some way into the
    // window (the server caps the series at maxBars from the end). Demand the
    // same fuzzy match the viewport mapper uses — the first bar at or after the
    // pattern bar's time — rather than an exact timeMs, so the anchor frame
    // degrades to the nearest entry bar instead of silently vanishing.
    const entryIndex = entryChart.bars.findIndex(
      (bar) => bar.timeMs >= patternTime,
    )
    return entryIndex >= 0 ? entryIndex : entryChart.bars.length - 1
  }, [scaledDown, entryChart, chart, anchorIndex])

  const useDetectedMatch = useCallback(
    (match: SetupMatch) => {
      if (!chart) return
      setFocusedMatch(match)
      setStatus({
        tone: 'good',
        message:
          'Viewing one historical result; your source drawing is unchanged.',
      })
    },
    [chart],
  )

  const testMarkets = useCallback(async () => {
    if (!drawing || !chart || comparisonSymbols.length === 0) return
    const range = windowFor(years)
    const stopFraction = Math.abs(drawing.entry - drawing.stop) / drawing.entry
    const targetFraction =
      Math.abs(drawing.target - drawing.entry) / drawing.entry
    setComparing(true)
    setPreview(null)
    setMarketResults(
      comparisonSymbols.map((targetSymbol) => ({
        symbol: targetSymbol,
        stage: 'queued',
        trades: 0,
        wins: 0,
        losses: 0,
        expectancy: 0,
      })),
    )
    revealResults()
    setStatus({
      tone: 'busy',
      message: `Testing ${comparisonSymbols.length} ${comparisonSymbols.length === 1 ? 'market' : 'markets'}…`,
    })

    const updateMarket = (targetSymbol: string, patch: Partial<MarketResult>) =>
      setMarketResults((current) =>
        current.map((item) =>
          item.symbol === targetSymbol ? { ...item, ...patch } : item,
        ),
      )

    const completed: Array<MarketResult> = []
    let cursor = 0
    const worker = async () => {
      while (cursor < comparisonSymbols.length) {
        const targetSymbol = comparisonSymbols[cursor]
        cursor += 1
        try {
          let targetChart = chart
          if (targetSymbol !== chart.symbol) {
            updateMarket(targetSymbol, { stage: 'preparing' })
            targetChart = await loadSetupChart({
              symbol: targetSymbol,
              timeframe,
              fromMs: range.fromMs,
              toMs: range.toMs,
              maxBars: MAX_BARS,
            })
          }
          updateMarket(targetSymbol, { stage: 'evaluating' })
          const sample = targetChart.bars.at(-2) ?? targetChart.bars.at(-1)
          if (!sample) throw new Error('No prepared candles')
          const entry = sample.close
          const stopDistance = Math.max(1, Math.round(entry * stopFraction))
          const targetDistance = Math.max(1, Math.round(entry * targetFraction))
          const translated: Drawing =
            targetSymbol === chart.symbol
              ? drawing
              : {
                  ...drawing,
                  symbol: targetSymbol,
                  anchorTimeMs: sample.timeMs,
                  levelLow: sample.low,
                  levelHigh: sample.high,
                  entry,
                  stop:
                    side === 'long'
                      ? entry - stopDistance
                      : entry + stopDistance,
                  target:
                    side === 'long'
                      ? entry + targetDistance
                      : entry - targetDistance,
                  riskUnit: 'percent',
                }
          const result = await previewSetup(
            translated,
            range.fromMs,
            range.toMs,
          )
          if (targetSymbol === chart.symbol) setPreview(result)
          const next: MarketResult = {
            symbol: targetSymbol,
            stage: 'done',
            trades: result.metrics.Trades ?? result.matches.length,
            wins: result.metrics.Wins ?? 0,
            losses: result.metrics.Losses ?? 0,
            expectancy: result.metrics.Expectancy?.Pessimistic ?? 0,
          }
          completed.push(next)
          updateMarket(targetSymbol, next)
        } catch (error) {
          console.error(`[Setup Test] ${targetSymbol} comparison failed`, error)
          const next: MarketResult = {
            symbol: targetSymbol,
            stage: 'error',
            trades: 0,
            wins: 0,
            losses: 0,
            expectancy: 0,
            warning: friendlySetupError(error, 'test'),
          }
          completed.push(next)
          updateMarket(targetSymbol, next)
        }
      }
    }
    await Promise.all(
      Array.from({ length: Math.min(3, comparisonSymbols.length) }, () =>
        worker(),
      ),
    )
    setStatus({
      tone: completed.some((result) => !result.warning) ? 'good' : 'bad',
      message: `Tested ${completed.length} ${completed.length === 1 ? 'market' : 'markets'}`,
    })
    setComparing(false)
  }, [drawing, chart, comparisonSymbols, years, timeframe, side, revealResults])

  const save = useCallback(async () => {
    if (!drawing || !preview?.sampleMatched) return
    const range = windowFor(years)
    setSaving(true)
    setStatus({
      tone: 'busy',
      message: 'Running and saving the evidence record…',
    })
    try {
      const summary = await runSetup(drawing, range.fromMs, range.toMs, {
        initialEquity: Number(sessionState.assumptions.initialEquity),
        riskPerTrade: drawing.riskPerTrade,
        spreadTicks: Number(sessionState.assumptions.spreadTicks),
        slippageTicks: Number(sessionState.assumptions.slippageTicks),
      })
      if (summary.error) throw new Error(summary.error)
      const runId = summary.run_id ?? ''
      if (!runId) throw new Error('The server saved no run id for this study.')
      // A setup test is a trial like any other. It has to enter the same ledger
      // the run log counts, or the disclosed trial count becomes a lie.
      session.recordRun(runId)
      void navigate({ to: '/runs/$runId', params: { runId } })
    } catch (error) {
      console.error('[Setup Test] Strategy save failed', error)
      setStatus({
        tone: 'bad',
        message: friendlySetupError(error, 'save'),
      })
    } finally {
      setSaving(false)
    }
  }, [drawing, preview, years, sessionState.assumptions, navigate])

  const kindMeta = SETUP_KINDS.find((item) => item.id === kind)!
  const tick = chart?.tickSize ?? '1'

  function updateRule(patch: Partial<RuleState>) {
    invalidate()
    setRule((current) => ({ ...current, ...patch }))
  }

  return (
    <div className="setup-workspace">
      <section className="setup-canvas">
        <header className="setup-toolbar">
          <div>
            {/* The spine is gone on this screen, so the way back has to live
                here. It is a link to the run log rather than browser history:
                the setup test can be reached from anywhere, and "back" should
                mean the study flow, not wherever the user happened to be. */}
            <Link to="/" className="setup-back">
              <ChevronLeft aria-hidden="true" />
              Run log
            </Link>
            <h1>Setup test</h1>
          </div>

          <div className="setup-toolbar-controls">
            <div className="setup-field">
              <label htmlFor="setup-market">Market</label>
              <Select
                value={symbol}
                onValueChange={(value) => {
                  setSymbol(value)
                  setChart(null)
                  setEntryChart(null)
                  setScaledDown(false)
                  setPreview(null)
                  setZonePlaced(false)
                  setEntryPlaced(false)
                  setPrecedentFromTimeMs(null)
                  setPatternViewport(null)
                  setEntryViewport(null)
                  chartCacheRef.current.clear()
                }}
              >
                <SelectTrigger id="setup-market" className="h-9">
                  <SelectValue placeholder="Market" />
                </SelectTrigger>
                <SelectContent>
                  {(instruments.length > 0
                    ? instruments
                    : [{ symbol, timeframes }]
                  ).map((item) => (
                    <SelectItem key={item.symbol} value={item.symbol}>
                      {displaySymbol(item.symbol)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="setup-field">
              <label htmlFor="setup-timeframe">Candles</label>
              <Select
                value={timeframe}
                onValueChange={(value) => {
                  setTimeframe(value)
                  setEntryTimeframe('')
                  setEntryChart(null)
                  setScaledDown(false)
                  setPreview(null)
                  setPatternViewport(null)
                  setEntryViewport(null)
                  chartCacheRef.current.clear()
                }}
              >
                <SelectTrigger id="setup-timeframe" className="h-9">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {timeframes.map((item) => (
                    <SelectItem key={item} value={item}>
                      {displayTimeframe(item)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="setup-field">
              <label htmlFor="setup-history">History</label>
              <Select
                value={String(years)}
                onValueChange={(value) => {
                  setYears(Number(value))
                  setPreview(null)
                  chartCacheRef.current.clear()
                }}
              >
                <SelectTrigger id="setup-history" className="h-9">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {HISTORY_YEARS.map((item) => (
                    <SelectItem key={item} value={String(item)}>
                      {item} {item === 1 ? 'year' : 'years'}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <Button
              className="h-9"
              variant="secondary"
              onClick={() => void prepare()}
              disabled={preparing}
            >
              {preparing ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <MousePointerClick className="size-4" />
              )}
              {chart ? 'Reload' : 'Prepare chart'}
            </Button>
          </div>

          <div className="setup-toolbar-end">
            <p
              className="setup-status"
              data-tone={status.tone}
              role="status"
              aria-live="polite"
            >
              {unavailable ?? status.message}
            </p>
            {/* Carried over from the original header. Without the spine, this
                is the only place the screen says what it is not. */}
            <p className="setup-historical">
              Historical simulation only · no order routing
            </p>
          </div>
        </header>

        {diagnostics && diagnostics.reasons.length > 0 ? (
          <div className="setup-notice" role="note">
            <AlertTriangle aria-hidden="true" />
            <div>
              <strong>
                This price data will not match your broker's chart
              </strong>
              <ul>
                {diagnostics.reasons.map((reason) => (
                  <li key={reason}>{reason}</li>
                ))}
              </ul>
            </div>
          </div>
        ) : null}

        <div className="setup-canvas-body" ref={chartRegionRef}>
          {chart && finerTimeframes.length > 0 ? (
            <Popover open={scaleOpen} onOpenChange={setScaleOpen}>
              <PopoverTrigger
                type="button"
                className="setup-scale-toggle"
                data-scaled={scaledDown}
                aria-label="Choose entry candles"
              >
                <ArrowDown aria-hidden="true" />
                {scaledDown
                  ? `Scale to ${displayTimeframe(timeframe)}`
                  : entryTimeframe
                    ? `Entry: ${displayTimeframe(entryTimeframe)}`
                    : 'Scale down to pick entry'}
              </PopoverTrigger>
              <PopoverContent
                align="end"
                sideOffset={8}
                className="setup-scale-menu"
              >
                <p className="setup-scale-menu-title">Entry candles</p>
                {finerTimeframes.map((rung) => (
                  <button
                    type="button"
                    key={rung}
                    className="setup-scale-rung"
                    data-active={
                      scaledDown && entryTimeframe === rung ? true : undefined
                    }
                    onClick={() => {
                      setScaleOpen(false)
                      void scaleTo(rung)
                    }}
                  >
                    {displayTimeframe(rung)}
                  </button>
                ))}
                {scaledDown ? (
                  <button
                    type="button"
                    className="setup-scale-rung"
                    onClick={() => {
                      setScaleOpen(false)
                      void scaleTo(timeframe)
                    }}
                  >
                    {displayTimeframe(timeframe)} · back on pattern candles
                  </button>
                ) : null}
              </PopoverContent>
            </Popover>
          ) : null}
          <SetupChart
            chart={displayChart}
            anchorIndex={displayAnchorIndex}
            lines={lines}
            side={side}
            showAnchor={setupMode === 'template' || entryPlaced}
            allowAnchorSelection={setupMode === 'template'}
            showRuleZone={setupMode === 'template'}
            matches={preview?.matches}
            focusedMatch={focusedMatch}
            precedentBars={drawing?.precedent.length ?? 0}
            drawingStorageKey={
              chart ? drawingStorageKeyFor(chart.symbol) : null
            }
            anchorLocked={scaledDown}
            disableDrawingTools={scaledDown}
            initialViewport={scaledDown ? entryViewport : patternViewport}
            onViewportChange={(vp) => {
              if (scaledDown) setEntryViewport(vp)
              else setPatternViewport(vp)
            }}
            onSelectAnchor={(index) => selectAnchor(index)}
            onChangeLines={(next) => {
              invalidate()
              setLines(next)
            }}
            onSelectDrawing={adoptVisualDrawing}
            onDeleteDrawing={(removed) => {
              invalidate()
              if (removed.type === 'zone') setZonePlaced(false)
              if (removed.type === 'zone') setPrecedentFromTimeMs(null)
              if (removed.type === 'long' || removed.type === 'short') {
                setEntryPlaced(false)
              }
            }}
          />
          <button
            type="button"
            className="setup-results-jump"
            data-ready={marketResults.length > 0}
            disabled={marketResults.length === 0}
            onClick={revealResults}
          >
            <ArrowDown aria-hidden="true" />
            {comparing
              ? 'View test progress'
              : marketResults.length > 0
                ? 'View test results'
                : 'Test results open below the chart'}
          </button>
        </div>

        <div className="setup-legend">
          <span data-line="entry">Entry</span>
          <span data-line="stop">Stop</span>
          <span data-line="target">Target</span>
          <span data-line="zone">Selected setup</span>
          {preview?.matches.length ? (
            <span>{preview.matches.length} simulated trades shown</span>
          ) : null}
          <em>
            Click to inspect · drag to pan · Ctrl/⌘ + scroll or pinch to zoom ·
            scroll normally to move between chart and results
          </em>
        </div>

        {preview || marketResults.length > 0 ? (
          <div
            className="setup-results"
            ref={resultsRef}
            tabIndex={-1}
            aria-label="Setup test results"
          >
            <header className="setup-results-toolbar">
              <div>
                <p className="runlog-label">Test results</p>
                <strong>
                  {comparing
                    ? 'Testing your setup across the selected markets…'
                    : 'Your setup test is ready to review'}
                </strong>
              </div>
              <Button size="sm" variant="outline" onClick={revealChart}>
                <ArrowUp className="size-4" />
                Back to chart
              </Button>
            </header>
            {preview ? (
              <PreviewResult
                preview={preview}
                tickSize={tick}
                onUseMatch={useDetectedMatch}
                focusedMatch={focusedMatch}
                onClearMatch={() => {
                  setFocusedMatch(null)
                  setStatus({
                    tone: 'good',
                    message: `${preview.matches.length} historical occurrences of this rule`,
                  })
                }}
              />
            ) : null}
            {marketResults.length > 0 ? (
              <MarketComparison results={marketResults} />
            ) : null}
          </div>
        ) : null}
      </section>

      <aside className="setup-rail">
        <section className="setup-rail-section">
          <p className="runlog-label">01 · Setup</p>
          <h2>Define the setup</h2>
          <div className="setup-mode" aria-label="Setup source">
            <Button
              size="sm"
              variant={setupMode === 'drawing' ? 'default' : 'outline'}
              onClick={() => {
                invalidate()
                setSetupMode('drawing')
                setKind('drawn_precedent')
              }}
            >
              Drawing
            </Button>
            <Button
              size="sm"
              variant={setupMode === 'template' ? 'default' : 'outline'}
              onClick={() => {
                invalidate()
                setSetupMode('template')
                if (kind === 'drawn_precedent') setKind('break_retest')
              }}
            >
              Rule template
            </Button>
          </div>

          {setupMode === 'drawing' ? (
            <>
              <p className="setup-hint setup-step-copy">
                Box the price action that defines your precedent, then place a
                Long entry or Short entry marker. Assay repeats its normalized
                candle shape—not a named setup or absolute price.
              </p>
              <div className="setup-readiness" role="status">
                <div data-ready={zonePlaced}>
                  {zonePlaced ? <CheckCircle2 /> : <span>1</span>}
                  <p>
                    <b>Precedent area</b>
                    <small>
                      {zonePlaced
                        ? 'Selected on chart'
                        : 'Use the Setup area tool'}
                    </small>
                  </p>
                </div>
                <div data-ready={entryPlaced}>
                  {entryPlaced ? <CheckCircle2 /> : <span>2</span>}
                  <p>
                    <b>Entry marker</b>
                    <small>
                      {entryPlaced
                        ? `${side === 'long' ? 'Long' : 'Short'} trigger placed`
                        : 'Use Long entry or Short entry'}
                    </small>
                  </p>
                </div>
              </div>
              {visualSource ? (
                <p className="setup-source-note">{visualSource}</p>
              ) : null}
            </>
          ) : (
            <div className="setup-kinds">
              {SETUP_KINDS.filter((item) => item.id !== 'drawn_precedent').map(
                (item) => (
                  <button
                    key={item.id}
                    type="button"
                    className="setup-kind"
                    aria-pressed={kind === item.id}
                    onClick={() => {
                      invalidate()
                      setKind(item.id)
                    }}
                  >
                    <b>{item.label}</b>
                    <span>{item.blurb}</span>
                  </button>
                ),
              )}
              <div className="setup-sides">
                {(['long', 'short'] as const).map((item) => (
                  <Button
                    key={item}
                    type="button"
                    size="sm"
                    variant={side === item ? 'default' : 'outline'}
                    onClick={() => {
                      invalidate()
                      setSide(item)
                      setLines((current) => {
                        if (!current) return current
                        const risk = Math.max(
                          1,
                          Math.abs(current.entry - current.stop),
                        )
                        const reward = Math.max(
                          1,
                          Math.abs(current.target - current.entry),
                        )
                        return {
                          ...current,
                          stop:
                            item === 'long'
                              ? current.entry - risk
                              : current.entry + risk,
                          target:
                            item === 'long'
                              ? current.entry + reward
                              : current.entry - reward,
                        }
                      })
                    }}
                  >
                    {item === 'long' ? 'Long' : 'Short'}
                  </Button>
                ))}
              </div>
            </div>
          )}
        </section>

        <section className="setup-rail-section">
          <p className="runlog-label">02 · Risk</p>
          <h2>Position</h2>
          <div className="setup-interpretation">
            {drawing && chart ? (
              <>
                <strong>
                  {side === 'long' ? 'Long' : 'Short'} · {kindMeta.label}
                </strong>
                <p>
                  Entry {formatTicks(drawing.entry, tick)} · Stop{' '}
                  {formatTicks(drawing.stop, tick)} · Target{' '}
                  {formatTicks(drawing.target, tick)}
                </p>
                <p className="text-muted-foreground">
                  {rr ? `${rr.toFixed(2)}R` : '—'} reward/risk. Distances are
                  translated by percentage across markets.
                </p>
              </>
            ) : (
              <p className="text-muted-foreground">
                Complete the setup drawing to define entry, stop and target.
              </p>
            )}
          </div>
          <div className="setup-risk-field">
            <label htmlFor="setup-risk">Risk per trade</label>
            <Select
              value={String(rule.riskPerTrade)}
              onValueChange={(value) =>
                updateRule({ riskPerTrade: Number(value) })
              }
            >
              <SelectTrigger id="setup-risk" className="mt-1.5 h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {RISK_CHOICES.map(([value, label]) => (
                  <SelectItem key={value} value={value}>
                    {label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </section>

        <section className="setup-rail-section">
          <p className="runlog-label">03 · Test</p>
          <h2>Find this setup</h2>
          <p className="setup-rule-summary">
            {drawing
              ? `Look for ${kindMeta.rule}.`
              : 'Complete the setup above to enable testing.'}
          </p>

          <details className="setup-disclosure">
            <summary>Advanced rules</summary>
            <div className="setup-fields">
              {kind === 'drawn_precedent' ? (
                <NumberField
                  id="setup-similarity"
                  label="Match similarity"
                  suffix="%"
                  hint="Higher values find fewer, closer candle-shape matches."
                  value={rule.similarityMilli / 10}
                  error={errors.similarityMilli}
                  onChange={(similarity) =>
                    updateRule({ similarityMilli: Math.round(similarity * 10) })
                  }
                />
              ) : kind === 'fair_value_gap_retest' ? (
                <>
                  <NumberField
                    id="setup-min-gap"
                    label="Minimum gap"
                    suffix="ticks"
                    hint="Smaller imbalances are ignored."
                    value={rule.minSizeTicks}
                    error={errors.minSizeTicks}
                    onChange={(minSizeTicks) => updateRule({ minSizeTicks })}
                  />
                  <div>
                    <label htmlFor="setup-fill-rule">Entry trigger</label>
                    <Select
                      value={rule.mitigationRule}
                      onValueChange={(value) =>
                        updateRule({
                          mitigationRule: value as RuleState['mitigationRule'],
                        })
                      }
                    >
                      <SelectTrigger
                        id="setup-fill-rule"
                        className="mt-1.5 h-9"
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="first_touch">First touch</SelectItem>
                        <SelectItem value="half_fill">Half fill</SelectItem>
                        <SelectItem value="full_fill">Full fill</SelectItem>
                      </SelectContent>
                    </Select>
                    <p className="setup-hint">
                      How far price returns into the FVG.
                    </p>
                  </div>
                </>
              ) : kind === 'poi_shift_entry' ? (
                <>
                  <NumberField
                    id="setup-lookback"
                    label="Zone lookback"
                    suffix="candles"
                    hint="How far back the displacement searches for the zone candle."
                    value={rule.lookbackBars}
                    error={errors.lookbackBars}
                    onChange={(lookbackBars) => updateRule({ lookbackBars })}
                  />
                  <NumberField
                    id="setup-tolerance"
                    label="Zone touch tolerance"
                    suffix="ticks"
                    hint={
                      chart
                        ? `About ${formatTicks(rule.toleranceTicks, tick)} of price.`
                        : 'Measured in instrument ticks.'
                    }
                    value={rule.toleranceTicks}
                    error={errors.toleranceTicks}
                    onChange={(toleranceTicks) =>
                      updateRule({ toleranceTicks })
                    }
                  />
                </>
              ) : (
                <>
                  <NumberField
                    id="setup-lookback"
                    label="Swing lookback"
                    suffix="candles"
                    hint="How far back the broken extreme is judged."
                    value={rule.lookbackBars}
                    error={errors.lookbackBars}
                    onChange={(lookbackBars) => updateRule({ lookbackBars })}
                  />
                  <NumberField
                    id="setup-retest"
                    label="Retest deadline"
                    suffix="candles"
                    hint="Maximum wait after the break."
                    value={rule.retestBars}
                    error={errors.retestBars}
                    onChange={(retestBars) => updateRule({ retestBars })}
                  />
                  <NumberField
                    id="setup-tolerance"
                    label="Retest tolerance"
                    suffix="ticks"
                    hint={
                      chart
                        ? `About ${formatTicks(rule.toleranceTicks, tick)} of price.`
                        : 'Measured in instrument ticks.'
                    }
                    value={rule.toleranceTicks}
                    error={errors.toleranceTicks}
                    onChange={(toleranceTicks) =>
                      updateRule({ toleranceTicks })
                    }
                  />
                </>
              )}
              {kind !== 'drawn_precedent' ? (
                <>
                  <NumberField
                    id="setup-expiry"
                    label="Maximum setup age"
                    suffix="candles"
                    hint="Older levels are ignored."
                    value={rule.expiryBars}
                    error={errors.expiryBars}
                    onChange={(expiryBars) => updateRule({ expiryBars })}
                  />
                  <label className="setup-check setup-full">
                    <input
                      type="checkbox"
                      className="size-4"
                      checked={rule.useBody}
                      onChange={(event) =>
                        updateRule({ useBody: event.target.checked })
                      }
                    />
                    <span>Use candle bodies only</span>
                  </label>
                  {kind !== 'poi_shift_entry' ? (
                    <label className="setup-check setup-full">
                      <input
                        type="checkbox"
                        className="size-4"
                        checked={rule.confirmByClose}
                        onChange={(event) =>
                          updateRule({ confirmByClose: event.target.checked })
                        }
                      />
                      <span>Require candle close</span>
                    </label>
                  ) : null}
                </>
              ) : null}
            </div>
          </details>

          <details className="setup-disclosure">
            <summary>Markets · {comparisonSymbols.length} selected</summary>
            <div className="setup-market-grid">
              {comparableMarkets.map((market) => (
                <label key={market.symbol} className="setup-check">
                  <input
                    type="checkbox"
                    checked={comparisonSymbols.includes(market.symbol)}
                    disabled={market.symbol === symbol}
                    onChange={(event) =>
                      setComparisonSymbols((current) =>
                        event.target.checked
                          ? [...new Set([...current, market.symbol])]
                          : current.filter((item) => item !== market.symbol),
                      )
                    }
                  />
                  <span>{displaySymbol(market.symbol)}</span>
                </label>
              ))}
            </div>
          </details>

          {errorList.length > 0 ? (
            <Alert variant="destructive" className="mt-4">
              <AlertTriangle className="size-4" />
              <AlertTitle>Fix the drawing first</AlertTitle>
              <AlertDescription>
                <ul className="list-disc space-y-1 pl-4">
                  {errorList.map((message) => (
                    <li key={message}>{message}</li>
                  ))}
                </ul>
              </AlertDescription>
            </Alert>
          ) : null}

          <div className="setup-actions">
            <Button
              onClick={() => void testMarkets()}
              disabled={
                !drawing ||
                errorList.length > 0 ||
                comparing ||
                comparisonSymbols.length === 0
              }
            >
              {comparing ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Play className="size-4" />
              )}
              Find this setup across markets
            </Button>
            <Button
              variant="outline"
              onClick={() => void save()}
              disabled={!preview?.sampleMatched || saving}
            >
              {saving ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Save className="size-4" />
              )}
              Save as strategy
            </Button>
          </div>

          <p className="setup-disclaimer">
            The drawing teaches a reusable rule, not a fixed price. Each market
            is evaluated independently on its prepared history. Test progress
            and results open automatically below the chart.
          </p>
        </section>
      </aside>
    </div>
  )
}

function MarketComparison({ results }: { results: Array<MarketResult> }) {
  return (
    <div className="setup-result">
      <div className="setup-result-head">
        <CheckCircle2 className="mt-0.5 size-5 shrink-0 text-success" />
        <div>
          <h2>Same visual rule across markets</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Each market is evaluated independently on its own prepared candles.
          </p>
        </div>
      </div>
      <div className="mt-4 overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Market</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Trades</TableHead>
              <TableHead>Won / lost</TableHead>
              <TableHead className="text-right">Average trade</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {results.map((result) => (
              <TableRow key={result.symbol}>
                <TableCell>{displaySymbol(result.symbol)}</TableCell>
                <TableCell>
                  <MarketStage stage={result.stage} />
                </TableCell>
                <TableCell>
                  {result.stage === 'done' ? result.trades : '—'}
                </TableCell>
                <TableCell>
                  {result.stage === 'error'
                    ? result.warning
                    : result.stage === 'done'
                      ? `${result.wins} / ${result.losses}`
                      : '—'}
                </TableCell>
                <TableCell className="text-right font-mono text-xs">
                  {result.stage === 'done'
                    ? formatCents(result.expectancy)
                    : '—'}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function MarketStage({ stage }: { stage: MarketResult['stage'] }) {
  const labels: Record<MarketResult['stage'], string> = {
    queued: 'Queued',
    preparing: 'Preparing data',
    evaluating: 'Evaluating',
    done: 'Done',
    error: 'Unavailable',
  }
  return (
    <span className="setup-market-stage" data-stage={stage}>
      {(stage === 'preparing' || stage === 'evaluating') && (
        <Loader2 className="size-3 animate-spin" />
      )}
      {labels[stage]}
    </span>
  )
}

function NumberField({
  id,
  label,
  suffix,
  hint,
  value,
  error,
  onChange,
}: {
  id: string
  label: string
  suffix: string
  hint: string
  value: number
  error?: string
  onChange: (value: number) => void
}) {
  return (
    <div>
      <label htmlFor={id}>
        {label} <span className="normal-case tracking-normal">({suffix})</span>
      </label>
      <Input
        id={id}
        type="number"
        inputMode="numeric"
        className="mt-1.5 h-9"
        value={value}
        onChange={(event) => onChange(Number(event.target.value))}
        aria-invalid={error ? true : undefined}
        aria-describedby={`${id}-hint`}
      />
      <p id={`${id}-hint`} className="setup-hint" data-error={Boolean(error)}>
        {error ?? hint}
      </p>
    </div>
  )
}

function PreviewResult({
  preview,
  tickSize,
  onUseMatch,
  focusedMatch,
  onClearMatch,
}: {
  preview: SetupPreview
  tickSize: string
  onUseMatch: (match: SetupMatch) => void
  focusedMatch: SetupMatch | null
  onClearMatch: () => void
}) {
  const metrics = preview.metrics
  const expectancy = metrics.Expectancy?.Pessimistic ?? 0
  const profitFactor = metrics.ProfitFactor?.Pessimistic ?? 0
  const trades = metrics.Trades ?? preview.matches.length
  const wins = metrics.Wins ?? 0
  const losses = metrics.Losses ?? 0

  return (
    <div className="setup-result">
      <div className="setup-result-head">
        {preview.sampleMatched ? (
          <CheckCircle2 className="mt-0.5 size-5 shrink-0 text-success" />
        ) : (
          <AlertTriangle className="mt-0.5 size-5 shrink-0 text-destructive" />
        )}
        <div>
          <h2>
            {preview.sampleMatched
              ? 'The rule describes your example'
              : 'The rule does not describe your example yet'}
            <span className="ml-2 text-sm font-normal text-muted-foreground">
              {preview.matches.length} historical occurrences
            </span>
          </h2>
          <p className="mt-1 text-sm text-muted-foreground">
            {preview.interpretation ||
              'Found by repeating the confirmed structural rule, not your price.'}
          </p>
        </div>
      </div>

      <div className="setup-metrics">
        <Metric
          label="Times this happened"
          value={String(trades)}
          hint={
            trades < 10
              ? 'Too few to conclude anything.'
              : 'Occurrences of the rule in this window.'
          }
        />
        <Metric
          label="Won / lost"
          value={`${wins} / ${losses}`}
          hint="On the worst-case reading of every ambiguous bar."
        />
        <Metric
          label="Average per trade"
          value={formatCents(expectancy)}
          hint="Worst case. What one trade returned on average."
        />
        <Metric
          label="Made per unit lost"
          value={profitFactor.toFixed(2)}
          hint="Worst case. Below 1.00 means it lost money."
        />
      </div>

      {preview.unresolved > 0 ? (
        <Alert className="mt-4">
          <AlertTriangle className="size-4" />
          <AlertTitle>
            {preview.unresolved} {preview.unresolved === 1 ? 'trade' : 'trades'}{' '}
            could not be resolved from these candles
          </AlertTitle>
          <AlertDescription>
            The bar's range contained both the stop and the target, and a candle
            cannot say which came first. Both readings are carried; the figures
            above show the unfavourable one.
          </AlertDescription>
        </Alert>
      ) : null}

      {(preview.warnings ?? []).map((warning) => {
        const sourceNotice = warning.startsWith('The source example')
        return (
          <Alert
            key={warning}
            variant={sourceNotice ? undefined : 'destructive'}
            className="mt-3"
          >
            {sourceNotice ? (
              <CheckCircle2 className="size-4 text-success" />
            ) : (
              <AlertTriangle className="size-4" />
            )}
            <AlertDescription>{warning}</AlertDescription>
          </Alert>
        )
      })}

      <p className="mt-3 text-xs leading-5 text-muted-foreground">
        Each occurrence preserves the percentage stop and target distances from
        your source drawing. If one candle can hit both, Assay labels the result
        ambiguous and counts the worst-case path first.
      </p>

      {preview.matches.length > 0 ? (
        <div className="mt-5 overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Signal</TableHead>
                <TableHead>Similarity</TableHead>
                <TableHead>Entry</TableHead>
                <TableHead>Stop</TableHead>
                <TableHead>Target</TableHead>
                <TableHead className="text-right">Worst case</TableHead>
                <TableHead className="text-right">Best case</TableHead>
                <TableHead>Example</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {preview.matches.slice(0, 15).map((match) => (
                <TableRow
                  key={`${match.signalTimeMs}-${match.entryTimeMs}`}
                  data-state={
                    focusedMatch?.signalTimeMs === match.signalTimeMs
                      ? 'selected'
                      : undefined
                  }
                >
                  <TableCell className="font-mono text-xs">
                    {new Date(match.signalTimeMs).toISOString().slice(0, 10)}
                    {match.ambiguous ? (
                      <Badge variant="outline" className="ml-2">
                        ambiguous
                      </Badge>
                    ) : null}
                  </TableCell>
                  <TableCell className="min-w-44 text-xs">
                    {match.similarityMilli ? (
                      <>
                        <strong className="font-mono text-foreground">
                          {(match.similarityMilli / 10).toFixed(1)}%
                        </strong>
                        <span className="mt-1 block text-[11px] leading-4 text-muted-foreground">
                          Shape {formatSimilarity(match.shapeMilli)} · Structure{' '}
                          {formatSimilarity(match.structureMilli)} · Candles{' '}
                          {formatSimilarity(match.directionMilli)} · Anatomy{' '}
                          {formatSimilarity(match.anatomyMilli)} · Trigger{' '}
                          {formatSimilarity(match.triggerMilli)}
                        </span>
                      </>
                    ) : (
                      <span className="text-muted-foreground">
                        Structural rule
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {formatTicks(match.entry, tickSize)}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {formatTicks(match.stop, tickSize)}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {formatTicks(match.target, tickSize)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs">
                    {formatCents(match.pessimisticPnl)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs">
                    {formatCents(match.optimisticPnl)}
                  </TableCell>
                  <TableCell>
                    {match.levelLow > 0 && match.levelHigh > 0 ? (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          focusedMatch?.signalTimeMs === match.signalTimeMs
                            ? onClearMatch()
                            : onUseMatch(match)
                        }
                      >
                        {focusedMatch?.signalTimeMs === match.signalTimeMs
                          ? 'Back to source'
                          : 'View on chart'}
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {preview.matches.length > 15 ? (
            <p className="mt-2 text-xs text-muted-foreground">
              Showing the first 15 of {preview.matches.length}. Save the study
              to keep the full record.
            </p>
          ) : null}
        </div>
      ) : null}

      <p className="setup-disclaimer">
        A backtest describes what a rule did on one historical sample. It does
        not describe what it will do.
      </p>
    </div>
  )
}

function formatSimilarity(value?: number) {
  return value == null ? '—' : `${(value / 10).toFixed(0)}%`
}

function Metric({
  label,
  value,
  hint,
}: {
  label: string
  value: string
  hint: string
}) {
  return (
    <div className="setup-metric">
      <p className="runlog-label">{label}</p>
      <b>{value}</b>
      <small>{hint}</small>
    </div>
  )
}

const money = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
})

function formatCents(value: number): string {
  return money.format(value / 100)
}

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type {
  KeyboardEvent as ReactKeyboardEvent,
  PointerEvent as ReactPointerEvent,
} from 'react'
import {
  BoxSelect,
  CaseSensitive,
  ChartNoAxesColumnIncreasing,
  CornerUpLeft,
  MousePointer2,
  MoveDiagonal2,
  Minus,
  TrendingDown,
  TrendingUp,
  Trash2,
} from 'lucide-react'
import type { ChartDrawing, ChartPoint, ChartTool } from '@/lib/chart-drawings'
import { drawingID } from '@/lib/chart-drawings'
import type {
  SetupChart as ChartData,
  SetupMatch,
  SetupSide,
} from '@/lib/setup'
import { formatTicks } from '@/lib/setup'
import { cn } from '@/lib/utils'

/**
 * The setup drawing surface.
 *
 * One canvas that does three jobs at once: it shows the candles, it lets the
 * user say which candle is the example, and it lets them place the entry, stop,
 * target and the level or zone by dragging. Everything on it is instrument
 * ticks — an integer — right up to the moment a number is painted.
 *
 * Two rules shaped the interaction:
 *
 *   1. A price the user did not intend is worse than no price. Dragging snaps
 *      to the open, high, low and close of the candle under the cursor, so a
 *      stop "at the low" is exactly the low rather than a pixel below it.
 *      Hold Alt to place a line freely.
 *   2. The drawing is an example, not the rule. The anchor candle is framed
 *      rather than highlighted like a selection, because what the study repeats
 *      is the structure around it, never this absolute price.
 *
 * The whole surface is keyboard-operable: arrow keys move the anchor, and once
 * a line is picked up they nudge it one tick at a time.
 */

export interface SetupLines {
  entry: number
  stop: number
  target: number
  levelLow: number
  levelHigh: number
}

export type LineName = keyof SetupLines

const LINE_ORDER: Array<LineName> = [
  'entry',
  'stop',
  'target',
  'levelHigh',
  'levelLow',
]

const LINE_LABEL: Record<LineName, string> = {
  entry: 'Entry',
  stop: 'Stop',
  target: 'Target',
  levelHigh: 'Level high',
  levelLow: 'Level low',
}

const PAD = { top: 16, right: 84, bottom: 28, left: 10 }
const HIT_RADIUS = 7
const SNAP_RADIUS = 6
const MIN_BARS = 20
/**
 * How many candles the chart opens on.
 *
 * The window fetched from the server is much longer than this on purpose — the
 * study runs over all of it. But a chart showing four hundred candles at once
 * draws each one about a pixel wide, which is a bar chart wearing a
 * candlestick's name: you cannot see a body, so you cannot see the thing the
 * setup is defined by. Open on a readable slice at the right-hand edge and let
 * the user zoom out if they want the shape of the whole window.
 */
const DEFAULT_VISIBLE = 120
const DRAWING_STORAGE = 'assay:setup-chart-drawings:v1'

const DRAWING_TOOLS: Array<{
  id: ChartTool
  label: string
  icon: typeof MousePointer2
}> = [
  { id: 'select', label: 'Select and move', icon: MousePointer2 },
  { id: 'level', label: 'Horizontal level', icon: Minus },
  { id: 'trend', label: 'Trend line', icon: MoveDiagonal2 },
  { id: 'zone', label: 'Setup area', icon: BoxSelect },
  { id: 'fib', label: 'Fibonacci range', icon: ChartNoAxesColumnIncreasing },
  { id: 'long', label: 'Long entry', icon: TrendingUp },
  { id: 'short', label: 'Short entry', icon: TrendingDown },
  { id: 'note', label: 'Note', icon: CaseSensitive },
]

interface Viewport {
  /** Index of the leftmost visible bar. */
  offset: number
  /** Number of visible bars. */
  count: number
}

interface ChartGeometry {
  plotW: number
  plotH: number
  step: number
  yFor: (value: number) => number
  valueAt: (y: number) => number
  xFor: (index: number) => number
  indexAt: (x: number) => number
}

interface Palette {
  up: string
  down: string
  entry: string
  stop: string
  target: string
  zone: string
  zoneFill: string
  grid: string
  axis: string
  muted: string
  surface: string
  anchor: string
}

function readPalette(element: HTMLElement): Palette {
  const style = getComputedStyle(element)
  const token = (name: string, fallback: string) => {
    const value = style.getPropertyValue(name).trim()
    return value ? `hsl(${value})` : fallback
  }
  const alpha = (name: string, a: number, fallback: string) => {
    const value = style.getPropertyValue(name).trim()
    return value ? `hsl(${value} / ${a})` : fallback
  }
  return {
    up: token('--chart-up', '#2f9e69'),
    down: token('--chart-down', '#d1495b'),
    entry: token('--chart-entry', '#3b82f6'),
    stop: token('--chart-stop', '#d1495b'),
    target: token('--chart-target', '#2f9e69'),
    zone: token('--chart-zone', '#c2870a'),
    zoneFill: alpha('--chart-zone', 0.14, 'rgba(194,135,10,.14)'),
    grid: alpha('--border', 0.75, 'rgba(120,140,150,.3)'),
    axis: token('--muted-foreground', '#7a8b93'),
    muted: token('--muted-foreground', '#7a8b93'),
    surface: token('--card', '#0d141b'),
    anchor: token('--foreground', '#e8eef2'),
  }
}

function timeLabel(ms: number, timeframe: string): string {
  const date = new Date(ms)
  const intraday = /^m|^h/i.test(timeframe)
  return intraday
    ? date.toLocaleString(undefined, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        timeZone: 'UTC',
      })
    : date.toLocaleDateString(undefined, {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
        timeZone: 'UTC',
      })
}

export function SetupChart({
  chart,
  anchorIndex,
  lines,
  side,
  showAnchor = false,
  allowAnchorSelection = false,
  showRuleZone = false,
  focusedMatch = null,
  precedentBars = 0,
  onSelectAnchor,
  onChangeLines,
  onSelectDrawing,
  onDeleteDrawing,
  matches = [],
  className,
  emptyMessage = 'Prepare the chart, then draw a zone and place an entry marker.',
}: {
  chart: ChartData | null
  anchorIndex: number | null
  lines: SetupLines | null
  side: SetupSide
  showAnchor?: boolean
  allowAnchorSelection?: boolean
  showRuleZone?: boolean
  focusedMatch?: SetupMatch | null
  precedentBars?: number
  onSelectAnchor: (index: number) => void
  onChangeLines: (lines: SetupLines) => void
  onSelectDrawing?: (drawing: ChartDrawing | null) => void
  onDeleteDrawing?: (drawing: ChartDrawing) => void
  matches?: Array<SetupMatch>
  className?: string
  emptyMessage?: string
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const wrapRef = useRef<HTMLDivElement>(null)
  const [viewport, setViewport] = useState<Viewport>({ offset: 0, count: 0 })
  const [hover, setHover] = useState<{ x: number; y: number } | null>(null)
  const [hoverLine, setHoverLine] = useState<LineName | null>(null)
  const [selected, setSelected] = useState<LineName | null>(null)
  const [dragging, setDragging] = useState<LineName | null>(null)
  const [snapped, setSnapped] = useState(false)
  const [tool, setTool] = useState<ChartTool>('select')
  const [noteText, setNoteText] = useState('Precedent')
  const [drawings, setDrawings] = useState<Array<ChartDrawing>>([])
  const [selectedDrawing, setSelectedDrawing] = useState<string | null>(null)
  const [draft, setDraft] = useState<ChartDrawing | null>(null)
  const [theme, setTheme] = useState(0)
  // Painting is decoupled from React renders and coalesced per frame; these
  // refs hold the state the paint path would otherwise recompute on every
  // pointer event.
  const paletteRef = useRef<Palette | null>(null)
  const sizeRef = useRef<{ width: number; height: number } | null>(null)
  const frameRef = useRef<number | null>(null)
  const undoRef = useRef<Array<Array<ChartDrawing>>>([])
  const draftRef = useRef<ChartDrawing | null>(null)
  const skipPersistRef = useRef(false)

  const bars = chart?.bars ?? []
  const barCount = bars.length
  const storageKey = chart
    ? `${DRAWING_STORAGE}:${chart.symbol}:${chart.timeframe}`
    : null
  const editableLines = useMemo(
    () =>
      showRuleZone
        ? LINE_ORDER
        : LINE_ORDER.filter(
            (name) => name !== 'levelHigh' && name !== 'levelLow',
          ),
    [showRuleZone],
  )

  useEffect(() => {
    if (!storageKey) {
      setDrawings([])
      return
    }
    try {
      const saved = localStorage.getItem(storageKey)
      skipPersistRef.current = true
      setDrawings(saved ? (JSON.parse(saved) as Array<ChartDrawing>) : [])
    } catch {
      skipPersistRef.current = true
      setDrawings([])
    }
    undoRef.current = []
    setSelectedDrawing(null)
    setDraft(null)
    draftRef.current = null
    setTool('select')
  }, [storageKey])

  useEffect(() => {
    if (!storageKey) return
    if (skipPersistRef.current) {
      skipPersistRef.current = false
      return
    }
    localStorage.setItem(storageKey, JSON.stringify(drawings))
  }, [drawings, storageKey])

  useEffect(() => {
    onSelectDrawing?.(
      selectedDrawing
        ? (drawings.find((drawing) => drawing.id === selectedDrawing) ?? null)
        : null,
    )
  }, [drawings, selectedDrawing, onSelectDrawing])

  // A new chart resets the view. Keeping a previous offset would silently show
  // a different window than the one the user just asked for.
  useEffect(() => {
    const count = Math.min(DEFAULT_VISIBLE, barCount)
    setViewport({ offset: Math.max(0, barCount - count), count })
    setSelected(null)
  }, [chart?.symbol, chart?.timeframe, chart?.fromMs, chart?.toMs, barCount])

  useEffect(() => {
    if (anchorIndex === null || barCount === 0) return
    setViewport((current) => {
      const count = current.count || Math.min(DEFAULT_VISIBLE, barCount)
      if (anchorIndex >= current.offset && anchorIndex < current.offset + count)
        return current
      return {
        count,
        offset: Math.max(
          0,
          Math.min(barCount - count, anchorIndex - Math.floor(count / 2)),
        ),
      }
    })
  }, [anchorIndex, barCount])

  // Colours come from the theme tokens, so a system theme flip has to repaint.
  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const bump = () => setTheme((n) => n + 1)
    media.addEventListener('change', bump)
    return () => media.removeEventListener('change', bump)
  }, [])

  // Reading the CSS custom properties calls getComputedStyle, which forces a
  // style recalculation. Do it once when the theme changes and cache the
  // result, rather than on every animation frame.
  useEffect(() => {
    if (wrapRef.current) paletteRef.current = readPalette(wrapRef.current)
  }, [theme])

  const view = useMemo(() => {
    if (barCount === 0) return { offset: 0, count: 0 }
    const count = Math.max(
      Math.min(MIN_BARS, barCount),
      Math.min(viewport.count || barCount, barCount),
    )
    const offset = Math.max(0, Math.min(viewport.offset, barCount - count))
    return { offset, count }
  }, [viewport, barCount])

  const visible = useMemo(
    () => bars.slice(view.offset, view.offset + view.count),
    [bars, view.offset, view.count],
  )

  const indexByTime = useMemo(
    () => new Map(bars.map((bar, index) => [bar.timeMs, index])),
    [bars],
  )

  useEffect(() => {
    if (!focusedMatch || barCount === 0) return
    const focusIndex = indexByTime.get(focusedMatch.signalTimeMs)
    if (focusIndex === undefined) return
    setViewport((current) => {
      const count = current.count || Math.min(DEFAULT_VISIBLE, barCount)
      return {
        count,
        offset: Math.max(
          0,
          Math.min(barCount - count, focusIndex - Math.floor(count / 2)),
        ),
      }
    })
  }, [focusedMatch, barCount, indexByTime])

  /** Price bounds include the drawn lines so a dragged line never leaves. */
  const bounds = useMemo(() => {
    if (visible.length === 0) return { lo: 0, hi: 1 }
    let lo = Infinity
    let hi = -Infinity
    for (const bar of visible) {
      if (bar.low < lo) lo = bar.low
      if (bar.high > hi) hi = bar.high
    }
    if (lines && (showAnchor || showRuleZone) && !focusedMatch) {
      for (const value of Object.values(lines)) {
        if (value < lo) lo = value
        if (value > hi) hi = value
      }
    }
    for (const drawing of focusedMatch ? [] : drawings) {
      for (const point of [drawing.from, drawing.to]) {
        const index = indexByTime.get(point.timeMs)
        if (
          index === undefined ||
          index < view.offset ||
          index >= view.offset + view.count
        )
          continue
        if (point.price < lo) lo = point.price
        if (point.price > hi) hi = point.price
      }
    }
    for (const match of matches) {
      const index = indexByTime.get(match.entryTimeMs)
      if (
        index === undefined ||
        index < view.offset ||
        index >= view.offset + view.count
      )
        continue
      for (const value of [
        match.entry,
        match.stop,
        match.target,
        match.exitPessimistic,
        match.exitOptimistic,
      ]) {
        if (!value) continue
        if (value < lo) lo = value
        if (value > hi) hi = value
      }
    }
    const pad = (hi - lo) * 0.08 || 1
    return { lo: lo - pad, hi: hi + pad }
  }, [
    visible,
    lines,
    drawings,
    matches,
    indexByTime,
    view,
    focusedMatch,
    showAnchor,
    showRuleZone,
  ])

  const geometry = useCallback(
    (width: number, height: number) => {
      const plotW = Math.max(1, width - PAD.left - PAD.right)
      const plotH = Math.max(1, height - PAD.top - PAD.bottom)
      const step = plotW / Math.max(1, visible.length)
      return {
        plotW,
        plotH,
        step,
        yFor: (value: number) =>
          PAD.top + ((bounds.hi - value) / (bounds.hi - bounds.lo)) * plotH,
        valueAt: (y: number) =>
          Math.round(
            bounds.hi - ((y - PAD.top) / plotH) * (bounds.hi - bounds.lo),
          ),
        xFor: (index: number) => PAD.left + (index + 0.5) * step,
        indexAt: (x: number) => Math.floor((x - PAD.left) / step) + view.offset,
      }
    },
    [visible.length, bounds, view.offset],
  )

  const draw = useCallback(() => {
    const canvas = canvasRef.current
    const wrap = wrapRef.current
    if (!canvas || !wrap) return
    const context = canvas.getContext('2d')
    if (!context) return
    const size =
      sizeRef.current ??
      (() => {
        const rect = wrap.getBoundingClientRect()
        return { width: rect.width, height: rect.height }
      })()
    const ratio = window.devicePixelRatio || 1
    const width = Math.max(1, size.width)
    const height = Math.max(1, size.height)
    if (canvas.width !== Math.round(width * ratio)) {
      canvas.width = Math.round(width * ratio)
    }
    if (canvas.height !== Math.round(height * ratio)) {
      canvas.height = Math.round(height * ratio)
    }
    context.setTransform(ratio, 0, 0, ratio, 0, 0)
    context.clearRect(0, 0, width, height)
    if (!chart || visible.length === 0) return

    const colors =
      paletteRef.current ?? (paletteRef.current = readPalette(wrap))
    const g = geometry(width, height)
    const tick = chart.tickSize
    context.font =
      '11px ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace'
    context.textBaseline = 'middle'

    // Price grid and the right-hand scale.
    const rows = 5
    context.lineWidth = 1
    for (let i = 0; i <= rows; i++) {
      const y = Math.round(PAD.top + (i * g.plotH) / rows) + 0.5
      context.strokeStyle = colors.grid
      context.beginPath()
      context.moveTo(PAD.left, y)
      context.lineTo(width - PAD.right, y)
      context.stroke()
      const value = bounds.hi - (i * (bounds.hi - bounds.lo)) / rows
      context.fillStyle = colors.axis
      context.textAlign = 'left'
      context.fillText(
        formatTicks(Math.round(value), tick),
        width - PAD.right + 8,
        y,
      )
    }

    // Time scale: four evenly spaced labels, first and last inclusive.
    context.textAlign = 'center'
    context.fillStyle = colors.axis
    const ticksAcross = Math.min(4, visible.length)
    for (let i = 0; i < ticksAcross; i++) {
      const index = Math.round(
        (i * (visible.length - 1)) / Math.max(1, ticksAcross - 1),
      )
      const bar = visible[index]
      const x = Math.min(
        width - PAD.right - 30,
        Math.max(PAD.left + 30, g.xFor(index)),
      )
      context.fillText(
        timeLabel(bar.timeMs, chart.timeframe),
        x,
        height - PAD.bottom / 2,
      )
    }

    // Entry, stop and target are one position, not three market-wide price
    // levels. Keep their risk/reward shading local to the explicit marker.
    if (lines && showAnchor && anchorIndex !== null && !focusedMatch) {
      const local = anchorIndex - view.offset
      if (local >= 0 && local < visible.length) {
        const left = g.xFor(local)
        const right = Math.min(
          PAD.left + g.plotW,
          left + Math.max(90, g.step * 16),
        )
        const entryY = g.yFor(lines.entry)
        const stopY = g.yFor(lines.stop)
        const targetY = g.yFor(lines.target)
        context.globalAlpha = 0.1
        context.fillStyle = colors.stop
        context.fillRect(
          left,
          Math.min(entryY, stopY),
          right - left,
          Math.abs(stopY - entryY),
        )
        context.fillStyle = colors.target
        context.fillRect(
          left,
          Math.min(entryY, targetY),
          right - left,
          Math.abs(targetY - entryY),
        )
        context.globalAlpha = 1
      }
    }

    // Candles. Bodies are hollow when the candle closed up and filled when it
    // closed down — the classical candlestick, where the eye reads direction
    // from weight rather than from colour alone. The body width follows the
    // zoom instead of being capped, because a candlestick whose body is a
    // pixel wide has stopped being one.
    const body = Math.max(1, g.step * 0.7)
    const hairline = body >= 3
    visible.forEach((bar, index) => {
      const centre = g.xFor(index)
      const x = Math.round(centre) + 0.5
      const up = bar.close >= bar.open
      const color = up ? colors.up : colors.down
      context.strokeStyle = color
      context.fillStyle = color

      // Wick, drawn full height first so the body sits on top of it.
      context.beginPath()
      context.moveTo(x, Math.round(g.yFor(bar.high)) + 0.5)
      context.lineTo(x, Math.round(g.yFor(bar.low)) + 0.5)
      context.stroke()

      const openY = g.yFor(bar.open)
      const closeY = g.yFor(bar.close)
      const top = Math.min(openY, closeY)
      const candleHeight = Math.abs(closeY - openY)
      const left = Math.round(centre - body / 2)
      const bodyWidth = Math.max(1, Math.round(body))

      if (candleHeight < 1 || !hairline) {
        // A doji, or a chart zoomed too far out for a body to hold an
        // outline: draw the open-to-close span as a solid mark rather than a
        // rectangle that would render as an empty box.
        context.fillRect(
          left,
          Math.round(top),
          bodyWidth,
          Math.max(1, candleHeight),
        )
        return
      }
      if (up) {
        // Painted with the surface colour rather than cleared: clearRect would
        // punch a transparent hole through the card and show the page behind
        // it wherever a candle crosses the zone shading.
        context.fillStyle = colors.surface
        context.fillRect(
          left,
          Math.round(top),
          bodyWidth,
          Math.round(candleHeight),
        )
        context.fillStyle = color
        context.strokeRect(
          left + 0.5,
          Math.round(top) + 0.5,
          bodyWidth - 1,
          Math.round(candleHeight) - 1,
        )
      } else {
        context.fillRect(
          left,
          Math.round(top),
          bodyWidth,
          Math.round(candleHeight),
        )
      }
    })

    context.save()
    context.beginPath()
    context.rect(PAD.left, PAD.top, g.plotW, g.plotH)
    context.clip()

    // Preview trades are engine output, not annotations. Entry and exit are
    // connected so the chart shows the lifetime of the simulated position;
    // stop/target guides stay faint enough not to become another price grid.
    for (const match of matches) {
      paintTradeMatch(
        context,
        match,
        side,
        g,
        indexByTime,
        view.offset,
        visible.length,
        colors,
      )
    }

    if (focusedMatch) {
      paintFocusedOccurrence(
        context,
        focusedMatch,
        side,
        precedentBars,
        g,
        indexByTime,
        view.offset,
        visible.length,
        colors,
        tick,
      )
    }

    if (!focusedMatch) {
      for (const drawing of draft ? [...drawings, draft] : drawings) {
        paintDrawing(
          context,
          drawing,
          g,
          indexByTime,
          view.offset,
          drawing.id === selectedDrawing || drawing.id === draft?.id,
          colors,
        )
      }
    }
    context.restore()

    // The anchor candle is framed, not filled — it marks the example, and an
    // example must not look like a result.
    if (showAnchor && anchorIndex !== null && !focusedMatch) {
      const local = anchorIndex - view.offset
      if (local >= 0 && local < visible.length) {
        const x = Math.round(g.xFor(local)) + 0.5
        const w = Math.max(6, g.step * 0.9)
        context.strokeStyle = colors.anchor
        context.setLineDash([3, 3])
        context.strokeRect(x - w / 2, PAD.top - 6, w, g.plotH + 12)
        context.setLineDash([])
        context.fillStyle = colors.anchor
        context.textAlign = 'center'
        context.fillText('entry trigger', x, PAD.top - 12)
      }
    }

    // Crosshair, under the lines so a line being dragged stays readable.
    if (hover && !dragging) {
      context.strokeStyle = colors.muted
      context.setLineDash([2, 3])
      context.beginPath()
      context.moveTo(PAD.left, Math.round(hover.y) + 0.5)
      context.lineTo(width - PAD.right, Math.round(hover.y) + 0.5)
      context.moveTo(Math.round(hover.x) + 0.5, PAD.top)
      context.lineTo(Math.round(hover.x) + 0.5, height - PAD.bottom)
      context.stroke()
      context.setLineDash([])
      pill(
        context,
        width - PAD.right + 2,
        hover.y,
        formatTicks(g.valueAt(hover.y), tick),
        colors.muted,
        colors.surface,
        'left',
      )
    }

    // Drawn lines and their handles.
    if (lines && !focusedMatch) {
      for (const name of editableLines) {
        const value = lines[name]
        const y = Math.round(g.yFor(value)) + 0.5
        const zone = name === 'levelHigh' || name === 'levelLow'
        const color = zone ? colors.zone : colors[name]
        const active =
          dragging === name || hoverLine === name || selected === name
        context.strokeStyle = color
        context.lineWidth = active ? 2 : 1
        context.setLineDash(zone ? [6, 4] : [])
        const local = anchorIndex === null ? -1 : anchorIndex - view.offset
        const start =
          local >= 0 && local < visible.length ? g.xFor(local) : PAD.left
        const end = Math.min(
          width - PAD.right,
          start + Math.max(90, g.step * 16),
        )
        context.beginPath()
        context.moveTo(start, y)
        context.lineTo(end, y)
        context.stroke()
        context.setLineDash([])
        context.lineWidth = 1
        pill(
          context,
          Math.min(width - PAD.right - 2, end),
          y,
          `${LINE_LABEL[name]} ${formatTicks(value, tick)}`,
          color,
          colors.surface,
          'right',
          active,
        )
      }
    }

    // Hover readout: the candle under the cursor, in full.
    if (hover) {
      const index = g.indexAt(hover.x) - view.offset
      if (index >= 0 && index < visible.length) {
        const bar = visible[index]
        const text = [
          timeLabel(bar.timeMs, chart.timeframe),
          `O ${formatTicks(bar.open, tick)}`,
          `H ${formatTicks(bar.high, tick)}`,
          `L ${formatTicks(bar.low, tick)}`,
          `C ${formatTicks(bar.close, tick)}`,
        ].join('   ')
        context.textAlign = 'left'
        const w = context.measureText(text).width + 16
        context.fillStyle = colors.surface
        context.globalAlpha = 0.92
        context.fillRect(PAD.left, 2, w, 20)
        context.globalAlpha = 1
        context.strokeStyle = colors.grid
        context.strokeRect(PAD.left + 0.5, 2.5, w, 19)
        context.fillStyle = colors.axis
        context.fillText(text, PAD.left + 8, 12)
      }
    }

    if (snapped && dragging) {
      context.fillStyle = colors.axis
      context.textAlign = 'right'
      context.fillText(
        'snapped to candle',
        width - PAD.right - 8,
        height - PAD.bottom - 8,
      )
    }
  }, [
    chart,
    visible,
    bounds,
    lines,
    anchorIndex,
    hover,
    hoverLine,
    selected,
    dragging,
    snapped,
    geometry,
    view.offset,
    theme,
    drawings,
    draft,
    selectedDrawing,
    matches,
    side,
    indexByTime,
    editableLines,
    showAnchor,
    showRuleZone,
    focusedMatch,
    precedentBars,
  ])

  // Painting is coalesced into one animation frame. A fast drag, pan or even a
  // hover fires many state updates in quick succession; without this, each one
  // repainted the whole canvas synchronously inside the pointer handler, so the
  // input queued behind the paint and the drawing visibly lagged the cursor.
  // Keeping the latest draw closure in a ref means a scheduled frame always
  // paints from the freshest state, whichever update triggered it.
  const drawRef = useRef(draw)
  drawRef.current = draw
  const schedule = useCallback(() => {
    if (frameRef.current !== null) return
    frameRef.current = requestAnimationFrame(() => {
      frameRef.current = null
      drawRef.current()
    })
  }, [])

  useEffect(() => {
    schedule()
  }, [draw, schedule])

  useEffect(
    () => () => {
      if (frameRef.current !== null) cancelAnimationFrame(frameRef.current)
    },
    [],
  )

  useEffect(() => {
    const wrap = wrapRef.current
    if (!wrap || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(() => {
      const rect = wrap.getBoundingClientRect()
      sizeRef.current = { width: rect.width, height: rect.height }
      schedule()
    })
    observer.observe(wrap)
    return () => observer.disconnect()
  }, [schedule])

  /* ------------------------------------------------------------ pointers */

  const pointFor = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    const rect = event.currentTarget.getBoundingClientRect()
    return { x: event.clientX - rect.left, y: event.clientY - rect.top, rect }
  }

  const lineAt = (x: number, y: number, rect: DOMRect): LineName | null => {
    if (!lines) return null
    const g = geometry(rect.width, rect.height)
    const local = anchorIndex === null ? -1 : anchorIndex - view.offset
    const start = local >= 0 && local < view.count ? g.xFor(local) : PAD.left
    const end = Math.min(
      rect.width - PAD.right,
      start + Math.max(90, g.step * 16),
    )
    if (x < start - HIT_RADIUS || x > end + HIT_RADIUS) return null
    let best: LineName | null = null
    let bestDistance = HIT_RADIUS
    for (const name of editableLines) {
      const distance = Math.abs(g.yFor(lines[name]) - y)
      if (distance < bestDistance) {
        best = name
        bestDistance = distance
      }
    }
    return best
  }

  const remember = () => {
    undoRef.current = [...undoRef.current.slice(-49), drawings]
  }

  const undo = () => {
    const previous = undoRef.current.at(-1)
    if (!previous) return
    undoRef.current = undoRef.current.slice(0, -1)
    setDrawings(previous)
    setSelectedDrawing(null)
  }

  const chartPointAt = (
    x: number,
    y: number,
    rect: DOMRect,
    free: boolean,
  ): ChartPoint | null => {
    if (barCount === 0) return null
    const g = geometry(rect.width, rect.height)
    const index = Math.max(0, Math.min(barCount - 1, g.indexAt(x)))
    const bar = bars[index]
    let price = g.valueAt(y)
    if (!free) {
      let distance = SNAP_RADIUS + 1
      for (const candidate of [bar.open, bar.high, bar.low, bar.close]) {
        const next = Math.abs(g.yFor(candidate) - y)
        if (next <= SNAP_RADIUS && next < distance) {
          price = candidate
          distance = next
        }
      }
      setSnapped(distance <= SNAP_RADIUS)
    }
    return { timeMs: bar.timeMs, price }
  }

  const drawingAt = (x: number, y: number, rect: DOMRect) => {
    const g = geometry(rect.width, rect.height)
    let best: {
      id: string
      handle: 'from' | 'to' | 'move'
      distance: number
    } | null = null
    for (const drawing of drawings) {
      const hit = hitDrawing(drawing, x, y, g, indexByTime, view.offset)
      if (hit && (!best || hit.distance < best.distance)) {
        best = { id: drawing.id, ...hit }
      }
    }
    return best
  }

  const panRef = useRef<{ x: number; offset: number; moved: boolean } | null>(
    null,
  )
  const drawingDragRef = useRef<{
    id: string
    handle: 'from' | 'to' | 'move'
    start: ChartPoint
    original: ChartDrawing
  } | null>(null)

  const onPointerDown = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    if (!chart) return
    const { x, y, rect } = pointFor(event)
    event.currentTarget.setPointerCapture(event.pointerId)

    if (tool !== 'select') {
      const point = chartPointAt(x, y, rect, event.altKey)
      if (!point) return
      const text =
        tool === 'note' || tool === 'zone'
          ? noteText.trim() || (tool === 'zone' ? 'Zone' : 'Note')
          : undefined
      const next: ChartDrawing = {
        id: drawingID(),
        type: tool,
        from: point,
        to: point,
        text,
      }
      if (
        tool === 'level' ||
        tool === 'long' ||
        tool === 'short' ||
        tool === 'note'
      ) {
        remember()
        setDrawings((current) => [...current, next])
        setSelectedDrawing(next.id)
        setTool('select')
      } else {
        draftRef.current = next
        setDraft(next)
      }
      return
    }

    const lineHit = lineAt(x, y, rect)
    if (lineHit) {
      setDragging(lineHit)
      setSelected(lineHit)
      setSelectedDrawing(null)
      return
    }
    const drawingHit = drawingAt(x, y, rect)
    if (drawingHit) {
      const original = drawings.find((drawing) => drawing.id === drawingHit.id)
      const start = chartPointAt(x, y, rect, true)
      if (original && start) {
        remember()
        drawingDragRef.current = {
          id: original.id,
          handle: drawingHit.handle,
          start,
          original,
        }
        setSelectedDrawing(original.id)
        setSelected(null)
      }
      return
    }
    setSelectedDrawing(null)
    panRef.current = { x, offset: view.offset, moved: false }
  }

  const onPointerMove = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    if (!chart) return
    const { x, y, rect } = pointFor(event)
    setHover({ x, y })
    const g = geometry(rect.width, rect.height)

    if (draftRef.current) {
      const point = chartPointAt(x, y, rect, event.altKey)
      if (point) {
        const next = { ...draftRef.current, to: point }
        draftRef.current = next
        setDraft(next)
      }
      return
    }

    const drawingDrag = drawingDragRef.current
    if (drawingDrag) {
      const point = chartPointAt(x, y, rect, event.altKey)
      if (!point) return
      setDrawings((current) =>
        current.map((drawing) => {
          if (drawing.id !== drawingDrag.id) return drawing
          if (drawingDrag.handle === 'from') return { ...drawing, from: point }
          if (drawingDrag.handle === 'to') return { ...drawing, to: point }
          if (
            drawing.type === 'long' ||
            drawing.type === 'short' ||
            drawing.type === 'note'
          ) {
            return { ...drawing, from: point, to: point }
          }
          const startIndex = indexByTime.get(drawingDrag.start.timeMs) ?? 0
          const pointIndex = indexByTime.get(point.timeMs) ?? startIndex
          const deltaIndex = pointIndex - startIndex
          const deltaPrice = point.price - drawingDrag.start.price
          const move = (source: ChartPoint): ChartPoint => {
            const sourceIndex = indexByTime.get(source.timeMs) ?? 0
            const bar =
              bars[
                Math.max(0, Math.min(barCount - 1, sourceIndex + deltaIndex))
              ]
            return { timeMs: bar.timeMs, price: source.price + deltaPrice }
          }
          return {
            ...drawing,
            from: move(drawingDrag.original.from),
            to: move(drawingDrag.original.to),
          }
        }),
      )
      return
    }

    if (dragging && lines) {
      let value = g.valueAt(y)
      let didSnap = false
      if (!event.altKey) {
        const localIndex = g.indexAt(x) - view.offset
        if (localIndex >= 0 && localIndex < visible.length) {
          const bar = visible[localIndex]
          for (const candidate of [bar.open, bar.high, bar.low, bar.close]) {
            if (Math.abs(g.yFor(candidate) - y) <= SNAP_RADIUS) {
              value = candidate
              didSnap = true
              break
            }
          }
        }
      }
      setSnapped(didSnap)
      onChangeLines(normalize({ ...lines, [dragging]: value }))
      return
    }

    const pan = panRef.current
    if (pan) {
      const moved = x - pan.x
      if (Math.abs(moved) > 3) pan.moved = true
      if (pan.moved) {
        const shift = Math.round(moved / Math.max(1, g.step))
        setViewport((current) => {
          const count = current.count || barCount
          return {
            count,
            offset: Math.max(0, Math.min(barCount - count, pan.offset - shift)),
          }
        })
      }
      return
    }

    setHoverLine(lineAt(x, y, rect))
  }

  const onPointerUp = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    const finishedDraft = draftRef.current
    if (finishedDraft) {
      remember()
      setDrawings((current) => [...current, finishedDraft])
      setSelectedDrawing(finishedDraft.id)
      setDraft(null)
      draftRef.current = null
      setTool('select')
      setSnapped(false)
      return
    }
    if (drawingDragRef.current) {
      drawingDragRef.current = null
      setSnapped(false)
      return
    }
    const pan = panRef.current
    panRef.current = null
    setDragging(null)
    setSnapped(false)
    if (!chart || !pan || pan.moved || !allowAnchorSelection) return
    const { x, rect } = pointFor(event)
    const g = geometry(rect.width, rect.height)
    const index = g.indexAt(x)
    if (index >= 0 && index < barCount) {
      setSelected(null)
      onSelectAnchor(index)
    }
  }

  // Modified-wheel zoom keeps the bar under the cursor in place. An ordinary
  // wheel gesture is deliberately left alone so the setup workspace can
  // scroll to the results below the chart; trackpad pinch arrives as a
  // ctrl-modified wheel event in browsers and continues to zoom naturally.
  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas || barCount === 0) return
    const handler = (event: WheelEvent) => {
      if (!event.ctrlKey && !event.metaKey) return
      event.preventDefault()
      const rect = canvas.getBoundingClientRect()
      const g = geometry(rect.width, rect.height)
      const focus = g.indexAt(event.clientX - rect.left)
      setViewport((current) => {
        const count = current.count || barCount
        const next = Math.max(
          Math.min(MIN_BARS, barCount),
          Math.min(
            barCount,
            Math.round(count * (event.deltaY > 0 ? 1.15 : 0.87)),
          ),
        )
        const ratio = (focus - current.offset) / Math.max(1, count)
        const offset = Math.max(
          0,
          Math.min(barCount - next, Math.round(focus - ratio * next)),
        )
        return { offset, count: next }
      })
    }
    canvas.addEventListener('wheel', handler, { passive: false })
    return () => canvas.removeEventListener('wheel', handler)
  }, [barCount, geometry])

  /* ----------------------------------------------------------- keyboard */

  const onKeyDown = (event: ReactKeyboardEvent<HTMLCanvasElement>) => {
    if (!chart) return
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'z') {
      event.preventDefault()
      undo()
      return
    }
    if (
      selectedDrawing &&
      (event.key === 'Delete' || event.key === 'Backspace')
    ) {
      event.preventDefault()
      remember()
      setDrawings((current) =>
        current.filter((drawing) => drawing.id !== selectedDrawing),
      )
      setSelectedDrawing(null)
      return
    }
    const step = event.shiftKey ? 10 : 1
    if (
      selected &&
      lines &&
      (event.key === 'ArrowUp' || event.key === 'ArrowDown')
    ) {
      event.preventDefault()
      const delta = event.key === 'ArrowUp' ? step : -step
      onChangeLines(
        normalize({ ...lines, [selected]: lines[selected] + delta }),
      )
      return
    }
    if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
      event.preventDefault()
      const delta = event.key === 'ArrowRight' ? 1 : -1
      const next = Math.max(
        0,
        Math.min(barCount - 1, (anchorIndex ?? 0) + delta),
      )
      onSelectAnchor(next)
      return
    }
    // "n" cycles which line the arrow keys move. Tab is deliberately left
    // alone: intercepting it would trap keyboard focus inside the canvas with
    // no way out, which is a worse problem than the one it solves.
    if ((event.key === 'n' || event.key === 'N') && lines) {
      event.preventDefault()
      const at = selected ? LINE_ORDER.indexOf(selected) : -1
      setSelected(LINE_ORDER[(at + 1) % LINE_ORDER.length])
      return
    }
    if (event.key === 'Escape') {
      setSelected(null)
      setSelectedDrawing(null)
      setDraft(null)
      draftRef.current = null
      setTool('select')
    }
  }

  const description = useMemo(() => {
    if (chart && focusedMatch) {
      return [
        `${chart.symbol} ${chart.timeframe}, ${barCount} candles.`,
        `Viewing matched precedent at ${timeLabel(focusedMatch.signalTimeMs, chart.timeframe)}.`,
        `Result entry ${formatTicks(focusedMatch.entry, chart.tickSize)},`,
        `stop ${formatTicks(focusedMatch.stop, chart.tickSize)},`,
        `target ${formatTicks(focusedMatch.target, chart.tickSize)}.`,
        focusedMatch.ambiguous
          ? 'The candle outcome is ambiguous; the chart leads with the worst-case path.'
          : '',
      ]
        .filter(Boolean)
        .join(' ')
    }
    if (!chart || !lines || anchorIndex === null) return emptyMessage
    const bar = bars[anchorIndex]
    return [
      `${chart.symbol} ${chart.timeframe}, ${barCount} candles.`,
      showAnchor
        ? `Entry trigger ${timeLabel(bar.timeMs, chart.timeframe)}.`
        : '',
      `Entry ${formatTicks(lines.entry, chart.tickSize)},`,
      `stop ${formatTicks(lines.stop, chart.tickSize)},`,
      `target ${formatTicks(lines.target, chart.tickSize)},`,
      showRuleZone
        ? `level ${formatTicks(lines.levelLow, chart.tickSize)} to ${formatTicks(lines.levelHigh, chart.tickSize)}.`
        : '',
      `Side ${side}.`,
    ]
      .filter(Boolean)
      .join(' ')
  }, [
    chart,
    lines,
    anchorIndex,
    bars,
    barCount,
    side,
    emptyMessage,
    showAnchor,
    showRuleZone,
    focusedMatch,
  ])

  return (
    <div ref={wrapRef} className={cn('setup-chart', className)}>
      <div
        className="setup-drawing-toolbar"
        role="toolbar"
        aria-label="Chart drawing tools"
      >
        {DRAWING_TOOLS.map((item) => {
          const Icon = item.icon
          return (
            <button
              key={item.id}
              type="button"
              aria-label={item.label}
              aria-pressed={tool === item.id}
              title={item.label}
              disabled={!chart || Boolean(focusedMatch)}
              onClick={() => {
                setTool(item.id)
                setSelected(null)
                setDraft(null)
                draftRef.current = null
              }}
            >
              <Icon aria-hidden="true" />
            </button>
          )
        })}
        {tool === 'note' || tool === 'zone' ? (
          <input
            aria-label={tool === 'zone' ? 'Setup area label' : 'Note text'}
            value={noteText}
            onChange={(event) => setNoteText(event.target.value)}
            onPointerDown={(event) => event.stopPropagation()}
          />
        ) : null}
        <span aria-hidden="true" />
        <button
          type="button"
          aria-label="Undo drawing"
          title="Undo drawing (⌘Z)"
          disabled={
            !chart || Boolean(focusedMatch) || undoRef.current.length === 0
          }
          onClick={undo}
        >
          <CornerUpLeft aria-hidden="true" />
        </button>
        <button
          type="button"
          aria-label="Delete selected drawing"
          title="Delete selected drawing"
          disabled={!selectedDrawing || Boolean(focusedMatch)}
          onClick={() => {
            if (!selectedDrawing) return
            const removed = drawings.find(
              (drawing) => drawing.id === selectedDrawing,
            )
            remember()
            setDrawings((current) =>
              current.filter((drawing) => drawing.id !== selectedDrawing),
            )
            setSelectedDrawing(null)
            if (removed) onDeleteDrawing?.(removed)
          }}
        >
          <Trash2 aria-hidden="true" />
        </button>
      </div>
      <canvas
        ref={canvasRef}
        className="setup-chart-canvas"
        style={{
          cursor:
            dragging || hoverLine
              ? 'ns-resize'
              : tool !== 'select'
                ? 'crosshair'
                : selectedDrawing
                  ? 'move'
                  : chart
                    ? 'crosshair'
                    : 'default',
          touchAction: 'none',
        }}
        tabIndex={0}
        role="application"
        aria-label={`Setup chart. ${description}`}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={() => {
          panRef.current = null
          drawingDragRef.current = null
          setDragging(null)
          setDraft(null)
          draftRef.current = null
          setSnapped(false)
        }}
        onPointerLeave={() => {
          setHover(null)
          setHoverLine(null)
        }}
        onKeyDown={onKeyDown}
      />
      {!chart ? <div className="setup-chart-empty">{emptyMessage}</div> : null}
      <span className="sr-only" aria-live="polite">
        {description}
      </span>
    </div>
  )
}

/** Keep the zone the right way up however its edges were dragged. */
function normalize(lines: SetupLines): SetupLines {
  if (lines.levelLow <= lines.levelHigh) return lines
  return { ...lines, levelLow: lines.levelHigh, levelHigh: lines.levelLow }
}

function pill(
  context: CanvasRenderingContext2D,
  x: number,
  y: number,
  text: string,
  color: string,
  surface: string,
  align: 'left' | 'right',
  strong = false,
) {
  context.textAlign = align
  const width = context.measureText(text).width + 12
  const left = align === 'left' ? x : x - width
  context.fillStyle = surface
  context.globalAlpha = 0.94
  context.fillRect(left, y - 8, width, 16)
  context.globalAlpha = 1
  context.strokeStyle = color
  context.lineWidth = strong ? 1.5 : 1
  context.strokeRect(left + 0.5, y - 7.5, width - 1, 15)
  context.lineWidth = 1
  context.fillStyle = color
  context.fillText(text, align === 'left' ? left + 6 : x - 6, y)
}

function pointPixels(
  point: ChartPoint,
  geometry: ChartGeometry,
  indexByTime: Map<number, number>,
  offset: number,
) {
  const index = indexByTime.get(point.timeMs)
  if (index === undefined) return null
  return { x: geometry.xFor(index - offset), y: geometry.yFor(point.price) }
}

function distanceToSegment(
  x: number,
  y: number,
  from: { x: number; y: number },
  to: { x: number; y: number },
) {
  const dx = to.x - from.x
  const dy = to.y - from.y
  const length = dx * dx + dy * dy
  if (length === 0) return Math.hypot(x - from.x, y - from.y)
  const t = Math.max(
    0,
    Math.min(1, ((x - from.x) * dx + (y - from.y) * dy) / length),
  )
  return Math.hypot(x - (from.x + t * dx), y - (from.y + t * dy))
}

function hitDrawing(
  drawing: ChartDrawing,
  x: number,
  y: number,
  geometry: ChartGeometry,
  indexByTime: Map<number, number>,
  offset: number,
): { handle: 'from' | 'to' | 'move'; distance: number } | null {
  const from = pointPixels(drawing.from, geometry, indexByTime, offset)
  const to = pointPixels(drawing.to, geometry, indexByTime, offset)
  if (!from || !to) return null
  const fromDistance = Math.hypot(x - from.x, y - from.y)
  const toDistance = Math.hypot(x - to.x, y - to.y)
  if (
    drawing.type === 'long' ||
    drawing.type === 'short' ||
    drawing.type === 'note'
  ) {
    return fromDistance <= 18
      ? { handle: 'move', distance: fromDistance }
      : null
  }
  if (fromDistance <= HIT_RADIUS + 2)
    return { handle: 'from', distance: fromDistance }
  if (toDistance <= HIT_RADIUS + 2 && drawing.type !== 'level') {
    return { handle: 'to', distance: toDistance }
  }
  if (drawing.type === 'level') {
    const distance = Math.abs(y - from.y)
    return distance <= HIT_RADIUS ? { handle: 'move', distance } : null
  }
  if (drawing.type === 'zone' || drawing.type === 'fib') {
    const left = Math.min(from.x, to.x)
    const right = Math.max(from.x, to.x)
    const top = Math.min(from.y, to.y)
    const bottom = Math.max(from.y, to.y)
    if (x >= left && x <= right && y >= top && y <= bottom) {
      const distance = Math.min(x - left, right - x, y - top, bottom - y)
      return { handle: 'move', distance }
    }
  }
  const distance = distanceToSegment(x, y, from, to)
  return distance <= HIT_RADIUS ? { handle: 'move', distance } : null
}

function paintDrawing(
  context: CanvasRenderingContext2D,
  drawing: ChartDrawing,
  geometry: ChartGeometry,
  indexByTime: Map<number, number>,
  offset: number,
  selected: boolean,
  colors: Palette,
) {
  const from = pointPixels(drawing.from, geometry, indexByTime, offset)
  const to = pointPixels(drawing.to, geometry, indexByTime, offset)
  if (!from || !to) return
  const color =
    drawing.type === 'long'
      ? colors.up
      : drawing.type === 'short'
        ? colors.down
        : drawing.type === 'zone'
          ? colors.zone
          : drawing.type === 'fib'
            ? colors.entry
            : colors.anchor
  context.strokeStyle = color
  context.fillStyle = color
  context.lineWidth = selected ? 2 : 1.25
  context.setLineDash([])

  if (drawing.type === 'level') {
    context.beginPath()
    context.moveTo(PAD.left, from.y)
    context.lineTo(PAD.left + geometry.plotW, from.y)
    context.stroke()
  } else if (drawing.type === 'trend') {
    context.beginPath()
    context.moveTo(from.x, from.y)
    context.lineTo(to.x, to.y)
    context.stroke()
  } else if (drawing.type === 'zone') {
    const left = Math.min(from.x, to.x)
    const top = Math.min(from.y, to.y)
    const width = Math.abs(to.x - from.x)
    const height = Math.abs(to.y - from.y)
    context.globalAlpha = 0.12
    context.fillRect(left, top, width, height)
    context.globalAlpha = 1
    context.strokeRect(left, top, width, height)
    if (drawing.text) {
      context.globalAlpha = 1
      context.textAlign = 'left'
      context.fillText(drawing.text, left + 6, top + 14)
    }
  } else if (drawing.type === 'fib') {
    const levels = [0, 0.236, 0.382, 0.5, 0.618, 0.786, 1]
    context.font = '10px ui-monospace, SFMono-Regular, monospace'
    context.textAlign = 'left'
    for (const level of levels) {
      const y = from.y + (to.y - from.y) * level
      context.globalAlpha = level === 0.5 ? 1 : 0.68
      context.beginPath()
      context.moveTo(from.x, y)
      context.lineTo(to.x, y)
      context.stroke()
      context.fillText(String(level), Math.min(from.x, to.x) + 4, y - 7)
    }
    context.globalAlpha = 1
  } else if (drawing.type === 'long' || drawing.type === 'short') {
    const direction = drawing.type === 'long' ? -1 : 1
    context.beginPath()
    context.moveTo(from.x, from.y)
    context.lineTo(from.x - 6, from.y + direction * -10)
    context.lineTo(from.x + 6, from.y + direction * -10)
    context.closePath()
    context.fill()
    context.textAlign = 'center'
    context.fillText(
      drawing.type === 'long' ? 'LONG' : 'SHORT',
      from.x,
      from.y + direction * -20,
    )
  } else {
    context.beginPath()
    context.arc(from.x, from.y, 4, 0, Math.PI * 2)
    context.fill()
    context.textAlign = 'left'
    context.fillText(drawing.text || 'Note', from.x + 8, from.y)
  }

  if (selected) {
    for (const point of drawing.type === 'level' ? [from] : [from, to]) {
      context.fillStyle = colors.surface
      context.strokeStyle = color
      context.beginPath()
      context.arc(point.x, point.y, 4, 0, Math.PI * 2)
      context.fill()
      context.stroke()
    }
  }
  context.globalAlpha = 1
  context.lineWidth = 1
}

function paintFocusedOccurrence(
  context: CanvasRenderingContext2D,
  match: SetupMatch,
  side: SetupSide,
  precedentBars: number,
  geometry: ChartGeometry,
  indexByTime: Map<number, number>,
  offset: number,
  visibleCount: number,
  colors: Palette,
  tickSize: string,
) {
  const signalIndex = indexByTime.get(match.signalTimeMs)
  const entryIndex = indexByTime.get(match.entryTimeMs)
  const exitIndex = indexByTime.get(match.exitTimeMsPessimistic)
  if (
    signalIndex === undefined ||
    entryIndex === undefined ||
    exitIndex === undefined ||
    signalIndex < offset ||
    signalIndex >= offset + visibleCount
  )
    return

  const patternFrom = Math.max(0, signalIndex - Math.max(3, precedentBars) + 1)
  const left = geometry.xFor(patternFrom - offset)
  const signalX = geometry.xFor(signalIndex - offset)
  const entryX = geometry.xFor(entryIndex - offset)
  const exitX = geometry.xFor(exitIndex - offset)
  const guideEnd = Math.max(entryX + 80, exitX)

  if (match.levelLow > 0 && match.levelHigh > 0) {
    const top = geometry.yFor(match.levelHigh)
    const bottom = geometry.yFor(match.levelLow)
    context.fillStyle = colors.entry
    context.globalAlpha = 0.12
    context.fillRect(left, top, signalX - left, bottom - top)
    context.globalAlpha = 1
    context.strokeStyle = colors.entry
    context.lineWidth = 2
    context.strokeRect(left, top, signalX - left, bottom - top)
    context.fillStyle = colors.entry
    context.textAlign = 'left'
    context.fillText('Matched precedent', left + 6, top + 14)
  }

  const guides = [
    ['Result stop', match.stop, colors.stop],
    ['Result entry', match.entry, colors.entry],
    ['Result target', match.target, colors.target],
  ] as const
  for (const [label, price, color] of guides) {
    const y = geometry.yFor(price)
    context.strokeStyle = color
    context.lineWidth = label === 'Result entry' ? 2 : 1.5
    context.beginPath()
    context.moveTo(entryX, y)
    context.lineTo(guideEnd, y)
    context.stroke()
    pill(
      context,
      guideEnd,
      y,
      `${label} ${formatTicks(price, tickSize)}`,
      color,
      colors.surface,
      'right',
      true,
    )
  }

  context.setLineDash([4, 3])
  context.strokeStyle = colors.entry
  context.beginPath()
  context.moveTo(entryX, PAD.top)
  context.lineTo(entryX, PAD.top + geometry.plotH)
  context.stroke()
  context.setLineDash([])
  context.fillStyle = colors.entry
  context.textAlign = 'center'
  context.fillText(
    side === 'long' ? 'LONG RESULT' : 'SHORT RESULT',
    entryX,
    PAD.top + 12,
  )

  const loss = match.pessimisticPnl < 0
  const resultColor = loss ? colors.stop : colors.target
  context.fillStyle = resultColor
  context.fillRect(exitX - 4, geometry.yFor(match.exitPessimistic) - 4, 8, 8)
  context.textAlign = 'center'
  context.fillText(
    match.ambiguous
      ? loss
        ? 'Ambiguous · worst-case loss'
        : 'Ambiguous · worst-case win'
      : loss
        ? 'Stop hit'
        : 'Target / exit hit',
    exitX,
    geometry.yFor(match.exitPessimistic) - 12,
  )
  context.lineWidth = 1
  context.globalAlpha = 1
}

function paintTradeMatch(
  context: CanvasRenderingContext2D,
  match: SetupMatch,
  side: SetupSide,
  geometry: ChartGeometry,
  indexByTime: Map<number, number>,
  offset: number,
  visibleCount: number,
  colors: Palette,
) {
  const entryIndex = indexByTime.get(match.entryTimeMs)
  const exitIndex = indexByTime.get(match.exitTimeMsPessimistic)
  if (entryIndex === undefined || exitIndex === undefined) return
  if (exitIndex < offset || entryIndex >= offset + visibleCount) return
  const entry = {
    x: geometry.xFor(entryIndex - offset),
    y: geometry.yFor(match.entry),
  }
  const exit = {
    x: geometry.xFor(exitIndex - offset),
    y: geometry.yFor(match.exitPessimistic),
  }
  const profitable = match.pessimisticPnl >= 0
  const resultColor = profitable ? colors.up : colors.down

  context.globalAlpha = 0.3
  context.setLineDash([4, 4])
  for (const [price, color] of [
    [match.stop, colors.stop],
    [match.target, colors.target],
  ] as const) {
    context.strokeStyle = color
    context.beginPath()
    context.moveTo(entry.x, geometry.yFor(price))
    context.lineTo(exit.x, geometry.yFor(price))
    context.stroke()
  }
  context.globalAlpha = 0.8
  context.setLineDash([])
  context.strokeStyle = resultColor
  context.lineWidth = 1.5
  context.beginPath()
  context.moveTo(entry.x, entry.y)
  context.lineTo(exit.x, exit.y)
  context.stroke()

  context.fillStyle = side === 'long' ? colors.up : colors.down
  const direction = side === 'long' ? -1 : 1
  context.beginPath()
  context.moveTo(entry.x, entry.y)
  context.lineTo(entry.x - 5, entry.y + direction * -9)
  context.lineTo(entry.x + 5, entry.y + direction * -9)
  context.closePath()
  context.fill()
  context.font = '9px ui-monospace, SFMono-Regular, monospace'
  context.textAlign = 'center'
  context.fillText('E', entry.x, entry.y + (side === 'long' ? 14 : -14))

  context.fillStyle = resultColor
  context.fillRect(exit.x - 3.5, exit.y - 3.5, 7, 7)
  context.fillText('X', exit.x, exit.y - 11)

  const optimisticIndex = indexByTime.get(match.exitTimeMsOptimistic)
  if (
    match.ambiguous &&
    optimisticIndex !== undefined &&
    (optimisticIndex !== exitIndex ||
      match.exitOptimistic !== match.exitPessimistic)
  ) {
    const optimistic = {
      x: geometry.xFor(optimisticIndex - offset),
      y: geometry.yFor(match.exitOptimistic),
    }
    context.globalAlpha = 0.55
    context.setLineDash([3, 3])
    context.beginPath()
    context.moveTo(entry.x, entry.y)
    context.lineTo(optimistic.x, optimistic.y)
    context.stroke()
    context.setLineDash([])
    context.strokeRect(optimistic.x - 4, optimistic.y - 4, 8, 8)
  }
  context.globalAlpha = 1
  context.lineWidth = 1
}

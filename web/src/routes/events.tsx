import { createFileRoute } from '@tanstack/react-router'
import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Activity,
  BarChart3,
  CheckCircle2,
  CircleDollarSign,
  Database,
  Info,
  Loader2,
  Play,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
  TrendingDown,
  TrendingUp,
  XCircle,
} from 'lucide-react'
import type {
  EventStudyResponse,
  EventTrade,
  EventTradeSummary,
  EventWindowSummary,
} from '@/gen/assay/v1/service_pb'
import { DataReadiness } from '@/gen/assay/v1/service_pb'
import { PageHeader } from '@/components/page-header'
import { DateField } from '@/components/date-field'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
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
import { formatMs, shortHash } from '@/lib/codec'
import {
  sortTimeframes,
  supportedTimeframesForInstrument,
  timeframeMinutes,
} from '@/lib/constraints'
import { listEventSnapshots } from '@/lib/event-snapshots'
import type { EventSnapshotMeta } from '@/lib/event-snapshots'
import {
  dataApi,
  eventStudyApi,
  isUnimplemented,
  rpcErrorMessage,
} from '@/lib/rpc'
import { displaySymbol, displayTimeframe } from '@/lib/strategy'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/events')({ component: EventStudyScreen })

const DAY_MS = 86_400_000
type Mode = 'impact' | 'trade' | 'risk'
type RunPhase =
  | { status: 'idle' }
  | { status: 'preparing'; percent: number; stage: string }
  | { status: 'studying' }
  | { status: 'empty' | 'cancelled' }
  | { status: 'done'; result: EventStudyResponse; events: EventRow[] }
  | { status: 'rule-unavailable'; message: string }
  | { status: 'unsupported'; message: string }
  | { status: 'error'; message: string; symbol: string; timeframe: string }

interface InstrumentRow {
  symbol: string
  timeframes: string[]
}
interface EventRow {
  id: string
  occurredAtMs: bigint
  title: string
  actual: string
  forecast: string
}

const MODES = [
  [
    'impact',
    Activity,
    'Measure impact',
    'How unusually far did the market move?',
  ],
  [
    'trade',
    CircleDollarSign,
    'Test a trade',
    'Would one repeatable rule have made money?',
  ],
  [
    'risk',
    ShieldAlert,
    'Measure event risk',
    'What if I was already holding a position?',
  ],
] as const
const EVENT_KINDS = [
  ['US_NONFARM_PAYROLLS', 'Nonfarm payrolls (NFP)', 'NFP'],
  ['US_CPI', 'US inflation (CPI)', 'US CPI'],
  ['FOMC_RATE_DECISION', 'Fed rate decision', 'FOMC'],
  ['EMERGENCY_POLICY_ACTION', 'Emergency policy action', 'Policy action'],
  ['ELECTION', 'Election', 'Election'],
  ['REFERENDUM', 'Referendum', 'Referendum'],
  ['CONFLICT_ESCALATION', 'Conflict escalation', 'Conflict'],
  ['BANK_FAILURE', 'Bank failure', 'Bank failure'],
  ['EXCHANGE_FAILURE', 'Exchange failure', 'Exchange failure'],
] as const
const HORIZONS = [
  ['h1', '1 hour'],
  ['h4', '4 hours'],
  ['d1', '1 day'],
  ['w1', '1 week'],
] as const
const money = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})
const formatMoneyCents = (value: bigint | number) =>
  money.format(Number(value) / 100)
const signedMoneyCents = (value: bigint | number) =>
  `${Number(value) > 0 ? '+' : ''}${formatMoneyCents(value)}`
const percent = (value: number, signed = true) =>
  `${signed && value > 0 ? '+' : ''}${(value * 100).toFixed(2)}%`
const dollarsToCents = (value: string, fallback: number) => {
  const parsed = Number(value.replaceAll(',', ''))
  return BigInt(Math.round((Number.isFinite(parsed) ? parsed : fallback) * 100))
}

function rangeForSnapshot(
  snapshot: EventSnapshotMeta | undefined,
  years: number,
) {
  const now = new Date()
  const today = Date.UTC(
    now.getUTCFullYear(),
    now.getUTCMonth(),
    now.getUTCDate() + 1,
  )
  const end = Math.min(
    today,
    snapshot?.toMs ? Number(snapshot.toMs) + DAY_MS : today,
  )
  const start = new Date(end)
  start.setUTCFullYear(start.getUTCFullYear() - years)
  return {
    from: BigInt(Math.max(start.getTime(), Number(snapshot?.fromMs ?? 0n))),
    to: BigInt(end),
  }
}

function bestTimeframe(timeframes: string[], mode: Mode) {
  const wanted = mode === 'impact' ? 60 : 5
  const exactIndex = timeframes.findIndex(
    (item) => timeframeMinutes(item) === wanted,
  )
  if (exactIndex >= 0) return timeframes[exactIndex]
  const usableIndex = timeframes.findIndex(
    (item) => mode === 'impact' || (timeframeMinutes(item) ?? Infinity) <= 5,
  )
  return timeframes.at(usableIndex >= 0 ? usableIndex : 0) || ''
}

function EventStudyScreen() {
  const [mode, setMode] = useState<Mode>('trade')
  const [snapshots, setSnapshots] = useState<EventSnapshotMeta[]>([])
  const [snapshot, setSnapshot] = useState('')
  const [snapshotLoading, setSnapshotLoading] = useState(true)
  const [snapshotError, setSnapshotError] = useState<string | null>(null)
  const [instruments, setInstruments] = useState<InstrumentRow[]>([])
  const [instrumentError, setInstrumentError] = useState<string | null>(null)
  const [kind, setKind] = useState('US_NONFARM_PAYROLLS')
  const [symbol, setSymbol] = useState('')
  const [timeframe, setTimeframe] = useState('')
  const [fromMs, setFromMs] = useState<bigint | null>(null)
  const [toMs, setToMs] = useState<bigint | null>(null)
  const [rangePreset, setRangePreset] = useState<number | 'custom'>(1)
  const [horizon, setHorizon] = useState('h4')
  const [direction, setDirection] = useState('follow_initial')
  const [surpriseFilter, setSurpriseFilter] = useState('all')
  const [minimumSurprise, setMinimumSurprise] = useState('1')
  const [entryDelay, setEntryDelay] = useState('m5')
  const [startingBalance, setStartingBalance] = useState('10000')
  const [positionSize, setPositionSize] = useState('10000')
  const [stopLoss, setStopLoss] = useState('')
  const [takeProfit, setTakeProfit] = useState('')
  const [run, setRun] = useState<RunPhase>({ status: 'idle' })
  const resultsRef = useRef<HTMLDivElement>(null)
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    let cancelled = false
    void listEventSnapshots()
      .then((items) => {
        if (cancelled) return
        setSnapshots(items)
        if (items[0]) {
          setSnapshot(items[0].hash)
          const range = rangeForSnapshot(items[0], 1)
          setFromMs(range.from)
          setToMs(range.to)
        }
      })
      .catch((error: unknown) => setSnapshotError(rpcErrorMessage(error)))
      .finally(() => setSnapshotLoading(false))
    return () => {
      cancelled = true
    }
  }, [])

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
        const preferred = rows.findIndex(
          (item) => item.symbol === 'DUKAS:EURUSD',
        )
        const first = rows.at(preferred >= 0 ? preferred : 0)
        if (first?.symbol) {
          setSymbol(first.symbol)
          setTimeframe(bestTimeframe(first.timeframes, mode))
        }
      })
      .catch((error: unknown) => setInstrumentError(rpcErrorMessage(error)))
    return () => {
      cancelled = true
    }
  }, [])

  const selectedSnapshot = snapshots.find((item) => item.hash === snapshot)
  const selectedInstrument = instruments.find((item) => item.symbol === symbol)
  const timeframes = selectedInstrument?.timeframes ?? []
  const selectedKind =
    EVENT_KINDS.find((item) => item[0] === kind) ?? EVENT_KINDS[0]
  const fineEnough =
    mode === 'impact' || (timeframeMinutes(timeframe) ?? Infinity) <= 5
  const windowValid = fromMs !== null && toMs !== null && fromMs < toMs
  const ready = Boolean(
    snapshot && symbol && timeframe && windowValid && fineEnough,
  )
  const isRunning = run.status === 'preparing' || run.status === 'studying'
  const validHorizons = HORIZONS.filter(
    (item) =>
      (timeframeMinutes(item[0]) ?? 0) >= (timeframeMinutes(timeframe) ?? 0),
  )
  const resetResult = () =>
    setRun((current) =>
      current.status === 'preparing' || current.status === 'studying'
        ? current
        : { status: 'idle' },
    )

  const changeMode = (next: Mode) => {
    setMode(next)
    setRun({ status: 'idle' })
    setTimeframe(bestTimeframe(timeframes, next))
    if (next === 'risk') {
      setDirection('long')
      setEntryDelay('h1')
    }
    if (next === 'trade') {
      setDirection('follow_initial')
      setEntryDelay('m5')
    }
  }
  const changeTimeframe = (value: string) => {
    setTimeframe(value)
    if (
      mode === 'impact' &&
      (timeframeMinutes(horizon) ?? 0) < (timeframeMinutes(value) ?? 0)
    ) {
      const nextHorizon = HORIZONS.find(
        (item) =>
          (timeframeMinutes(item[0]) ?? 0) >= (timeframeMinutes(value) ?? 0),
      )
      if (nextHorizon) setHorizon(nextHorizon[0])
    }
    setRun({ status: 'idle' })
  }
  const applyRange = (years: number, target = selectedSnapshot) => {
    const range = rangeForSnapshot(target, years)
    setFromMs(range.from)
    setToMs(range.to)
    setRangePreset(years)
    resetResult()
  }
  const changeSymbol = (next: string) => {
    setSymbol(next)
    const instrument = instruments.find((item) => item.symbol === next)
    setTimeframe(bestTimeframe(instrument?.timeframes ?? [], mode))
    setRun({ status: 'idle' })
  }

  const ruleSentence = useMemo(() => {
    const horizonName = HORIZONS.find((item) => item[0] === horizon)?.[1]
    if (mode === 'impact')
      return `Measure the absolute ${horizonName} move after every ${selectedKind[2]} release and compare it with ordinary weeks.`
    if (mode === 'risk')
      return `Enter ${direction} ${entryDelay.toUpperCase()} before each ${selectedKind[2]} release and hold through the next ${horizon.toUpperCase()}.`
    const side =
      direction === 'follow_surprise'
        ? 'trade with the release surprise'
        : direction === 'follow_initial'
          ? 'follow the first reaction'
          : direction === 'fade_initial'
            ? 'fade the first reaction'
            : `go ${direction}`
    return `After each ${selectedKind[2]} release, wait ${entryDelay.toUpperCase()}, ${side}, and exit after ${horizon.toUpperCase()}.`
  }, [direction, entryDelay, horizon, mode, selectedKind])

  useEffect(resetResult, [
    snapshot,
    kind,
    symbol,
    timeframe,
    fromMs,
    toMs,
    horizon,
    direction,
    entryDelay,
    surpriseFilter,
    minimumSurprise,
    startingBalance,
    positionSize,
    stopLoss,
    takeProfit,
  ])

  const runStudy = async () => {
    if (!ready || fromMs === null || toMs === null) return
    const controller = new AbortController()
    abortRef.current = controller
    setRun({
      status: 'preparing',
      percent: 5,
      stage: `Finding ${selectedKind[2]} releases…`,
    })
    try {
      const filter = {
        fromMs,
        toMs,
        kinds: [kind],
        currencies: [],
        minimumImpact: '',
        asOfMs: 0n,
      }
      const query = await eventStudyApi.queryEvents(
        { snapshot, filter },
        { signal: controller.signal },
      )
      const events = query.events.map((event) => ({
        id: event.id,
        occurredAtMs: event.occurredAtMs,
        title:
          kind === 'US_NONFARM_PAYROLLS'
            ? 'U.S. Employment Situation release'
            : event.title,
        actual: event.actual,
        forecast: event.forecast,
      }))
      if (!events.length) {
        setRun({ status: 'empty' })
        return
      }
      const needsSurpriseData =
        mode === 'trade' &&
        (direction === 'follow_surprise' || surpriseFilter !== 'all')
      if (
        needsSurpriseData &&
        !events.some((event) => event.actual && event.forecast)
      ) {
        setRun({
          status: 'rule-unavailable',
          message:
            'This verified event snapshot does not include consensus forecasts. Choose Follow first reaction, Fade first reaction, Always long, or Always short.',
        })
        return
      }
      const preparationFrom =
        mode === 'risk' ? fromMs - BigInt(2 * DAY_MS) : fromMs
      const preparationTo = toMs + BigInt(8 * DAY_MS)
      try {
        const request = {
          symbol,
          timeframe: timeframe.toLowerCase(),
          fromMs: preparationFrom,
          toMs: preparationTo,
        }
        const status = await dataApi.dataStatus(request, {
          signal: controller.signal,
        })
        if (status.readiness !== DataReadiness.READY) {
          for await (const progress of dataApi.ensureData(request, {
            signal: controller.signal,
          })) {
            setRun({
              status: 'preparing',
              percent: Math.max(10, Math.min(90, progress.percent)),
              stage: progress.stage || 'Preparing price history…',
            })
          }
        }
      } catch (error) {
        if (!isUnimplemented(error)) throw error
      }
      setRun({ status: 'studying' })
      const trade =
        mode === 'impact'
          ? undefined
          : {
              entryTiming: mode === 'risk' ? 'before_release' : 'after_release',
              directionRule: direction,
              entryDelay,
              holdFor: horizon,
              minimumSurpriseZ: Number(minimumSurprise) || 0,
              surpriseFilter: mode === 'risk' ? 'all' : surpriseFilter,
              startingEquity: dollarsToCents(startingBalance, 10_000),
              positionSize: dollarsToCents(positionSize, 10_000),
              // Zero asks the local Assay service to estimate spread,
              // slippage, and commission from the selected instrument.
              costPerTrade: 0n,
              stopLoss: (Number(stopLoss) || 0) / 100,
              takeProfit: (Number(takeProfit) || 0) / 100,
            }
      const response = await eventStudyApi.runEventStudy(
        {
          snapshot,
          symbol,
          timeframe: timeframe.toLowerCase(),
          filter,
          windows: validHorizons.map((item) => item[0]),
          trade,
        },
        { signal: controller.signal },
      )
      setRun({ status: 'done', result: response, events })
      window.setTimeout(
        () => resultsRef.current?.scrollIntoView({ behavior: 'smooth' }),
        50,
      )
    } catch (error) {
      if (controller.signal.aborted) setRun({ status: 'cancelled' })
      else if (isUnimplemented(error))
        setRun({ status: 'unsupported', message: rpcErrorMessage(error) })
      else
        setRun({
          status: 'error',
          message: rpcErrorMessage(error),
          symbol,
          timeframe,
        })
    } finally {
      abortRef.current = null
    }
  }

  return (
    <div className="page-form">
      <PageHeader
        title="Economic event lab"
        description="Measure the reaction, test a repeatable news trade, or see the risk of holding through a release. Every answer uses snapshot-pinned events and price data."
      />
      <div className="grid gap-3 md:grid-cols-3" aria-label="Research goal">
        {MODES.map(([id, Icon, title, description]) => (
          <button
            key={id}
            type="button"
            onClick={() => changeMode(id)}
            className={cn(
              'rounded-xl border p-4 text-left transition-colors hover:border-foreground/40',
              mode === id
                ? 'border-foreground bg-foreground text-background shadow-sm'
                : 'bg-card',
            )}
            aria-pressed={mode === id}
          >
            <span className="mb-3 flex items-center justify-between">
              <Icon className="size-5" />
              {mode === id && <CheckCircle2 className="size-4" />}
            </span>
            <span className="block font-semibold">{title}</span>
            <span
              className={cn(
                'mt-1 block text-sm',
                mode === id ? 'text-background/70' : 'text-muted-foreground',
              )}
            >
              {description}
            </span>
          </button>
        ))}
      </div>
      <Card className="border-foreground/20">
        <CardHeader className="border-b bg-muted/25">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <CardTitle>Set up the research</CardTitle>
              <CardDescription className="mt-1">
                Start with the question. Advanced assumptions appear only when
                they affect the answer.
              </CardDescription>
            </div>
            <Badge variant="outline" className="gap-1.5">
              <ShieldCheck className="size-3.5" /> No look-ahead
            </Badge>
          </div>
        </CardHeader>
        <CardContent className="space-y-7 pt-6">
          <Section number="1" title="Choose the release and market">
            <div className="grid gap-4 md:grid-cols-2">
              <Field label="Economic event" htmlFor="event-kind">
                <Select value={kind} onValueChange={setKind}>
                  <SelectTrigger id="event-kind" className="h-11">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {EVENT_KINDS.map((item) => (
                      <SelectItem key={item[0]} value={item[0]}>
                        {item[1]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="Market" htmlFor="event-market">
                {instruments.length ? (
                  <Select value={symbol} onValueChange={changeSymbol}>
                    <SelectTrigger id="event-market" className="h-11">
                      <SelectValue placeholder="Choose a market" />
                    </SelectTrigger>
                    <SelectContent>
                      {instruments.map((item) => (
                        <SelectItem key={item.symbol} value={item.symbol}>
                          {displaySymbol(item.symbol)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : (
                  <Input
                    id="event-market"
                    value={symbol}
                    onChange={(event) => setSymbol(event.target.value)}
                    placeholder={
                      instrumentError ? 'e.g. DUKAS:EURUSD' : 'Loading markets…'
                    }
                  />
                )}
              </Field>
            </div>
          </Section>
          <Section number="2" title="Choose the history">
            <div className="flex flex-wrap gap-2">
              {[1, 3, 5, 10].map((years) => (
                <Button
                  key={years}
                  type="button"
                  size="sm"
                  variant={rangePreset === years ? 'default' : 'outline'}
                  onClick={() => applyRange(years)}
                >
                  {years}Y
                </Button>
              ))}
            </div>
            <div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <Field label="From" htmlFor="event-from">
                <DateField
                  id="event-from"
                  value={fromMs}
                  onChange={(value) => {
                    setFromMs(value)
                    setRangePreset('custom')
                  }}
                  className="h-11"
                />
              </Field>
              <Field label="To" htmlFor="event-to">
                <DateField
                  id="event-to"
                  value={toMs}
                  onChange={(value) => {
                    setToMs(value)
                    setRangePreset('custom')
                  }}
                  className="h-11"
                />
              </Field>
              <Field
                label={
                  mode === 'impact' ? 'Measurement bars' : 'Execution bars'
                }
                htmlFor="event-timeframe"
                hint={
                  mode === 'impact'
                    ? 'H1 is fast and suitable for broad reactions.'
                    : 'M1 or M5 is required for honest entry timing.'
                }
              >
                <Select value={timeframe} onValueChange={changeTimeframe}>
                  <SelectTrigger id="event-timeframe" className="h-11">
                    <SelectValue placeholder="Choose bars" />
                  </SelectTrigger>
                  <SelectContent>
                    {timeframes.map((item) => (
                      <SelectItem key={item} value={item}>
                        {displayTimeframe(item)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
          </Section>
          <Section
            number="3"
            title={
              mode === 'impact'
                ? 'Choose the reaction window'
                : 'Define one repeatable rule'
            }
          >
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
              <Field
                label={mode === 'impact' ? 'Focus horizon' : 'Exit after'}
                htmlFor="event-horizon"
              >
                <Select value={horizon} onValueChange={setHorizon}>
                  <SelectTrigger id="event-horizon" className="h-11">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {HORIZONS.map((item) => (
                      <SelectItem
                        key={item[0]}
                        value={item[0]}
                        disabled={
                          mode === 'impact' &&
                          (timeframeMinutes(item[0]) ?? 0) <
                            (timeframeMinutes(timeframe) ?? 0)
                        }
                      >
                        {item[1]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              {mode !== 'impact' && (
                <>
                  <Field
                    label={mode === 'risk' ? 'Position held' : 'Direction rule'}
                    htmlFor="event-direction"
                  >
                    <Select value={direction} onValueChange={setDirection}>
                      <SelectTrigger id="event-direction" className="h-11">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {mode === 'trade' && (
                          <>
                            <SelectItem value="follow_surprise">
                              Follow the surprise
                            </SelectItem>
                            <SelectItem value="follow_initial">
                              Follow first reaction
                            </SelectItem>
                            <SelectItem value="fade_initial">
                              Fade first reaction
                            </SelectItem>
                          </>
                        )}
                        <SelectItem value="long">Always long</SelectItem>
                        <SelectItem value="short">Always short</SelectItem>
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field
                    label={
                      mode === 'risk'
                        ? 'Enter before release'
                        : 'Wait after release'
                    }
                    htmlFor="event-delay"
                  >
                    <Select value={entryDelay} onValueChange={setEntryDelay}>
                      <SelectTrigger id="event-delay" className="h-11">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {mode === 'trade' ? (
                          <>
                            <SelectItem value="m1">1 minute</SelectItem>
                            <SelectItem value="m5">5 minutes</SelectItem>
                            <SelectItem value="m15">15 minutes</SelectItem>
                          </>
                        ) : (
                          <>
                            <SelectItem value="m15">15 minutes</SelectItem>
                            <SelectItem value="h1">1 hour</SelectItem>
                            <SelectItem value="h4">4 hours</SelectItem>
                          </>
                        )}
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field label="Releases to trade" htmlFor="event-surprise">
                    <Select
                      value={mode === 'risk' ? 'all' : surpriseFilter}
                      onValueChange={setSurpriseFilter}
                      disabled={mode === 'risk'}
                    >
                      <SelectTrigger id="event-surprise" className="h-11">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="all">Every release</SelectItem>
                        <SelectItem value="absolute">
                          Large surprises only
                        </SelectItem>
                        <SelectItem value="positive">
                          Positive surprises
                        </SelectItem>
                        <SelectItem value="negative">
                          Negative surprises
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </Field>
                </>
              )}
            </div>
            {mode !== 'impact' && (
              <div className="mt-5 grid gap-4 rounded-xl border bg-muted/20 p-4 sm:grid-cols-2 lg:grid-cols-3">
                <MoneyField
                  label="Starting account"
                  id="event-account"
                  value={startingBalance}
                  onChange={setStartingBalance}
                />
                <MoneyField
                  label="Exposure per release"
                  id="event-size"
                  value={positionSize}
                  onChange={setPositionSize}
                />
                <div className="rounded-lg border bg-background p-3 sm:col-span-2 lg:col-span-1">
                  <div className="text-sm font-medium">Trading costs</div>
                  <div className="mt-1 text-sm text-muted-foreground">
                    Estimated automatically from the selected market’s spread,
                    slippage, and commission assumptions. The estimate scales
                    with exposure.
                  </div>
                </div>
                <PercentField
                  label="Stop loss (optional)"
                  id="event-stop"
                  value={stopLoss}
                  onChange={setStopLoss}
                  placeholder="No stop"
                />
                <PercentField
                  label="Take profit (optional)"
                  id="event-target"
                  value={takeProfit}
                  onChange={setTakeProfit}
                  placeholder="No target"
                />
                {surpriseFilter !== 'all' && mode === 'trade' && (
                  <Field label="Minimum surprise" htmlFor="event-z">
                    <Input
                      id="event-z"
                      inputMode="decimal"
                      value={minimumSurprise}
                      onChange={(event) =>
                        setMinimumSurprise(event.target.value)
                      }
                    />
                  </Field>
                )}
              </div>
            )}
            <div className="mt-5 flex gap-3 rounded-lg border border-primary/20 bg-primary/5 p-4 text-sm">
              <Sparkles className="mt-0.5 size-4 shrink-0 text-primary" />
              <div>
                <div className="font-medium">Your test, in plain English</div>
                <p className="mt-1 text-muted-foreground">{ruleSentence}</p>
              </div>
            </div>
          </Section>
          {!fineEnough && (
            <Alert>
              <Info className="size-4" />
              <AlertTitle>Choose M1 or M5 execution bars</AlertTitle>
              <AlertDescription>
                {displayTimeframe(timeframe)} can describe a broad reaction but
                cannot honestly model a news entry.
              </AlertDescription>
            </Alert>
          )}
          {(snapshotError || instrumentError) && (
            <Alert variant="destructive">
              <XCircle className="size-4" />
              <AlertTitle>Some local data is unavailable</AlertTitle>
              <AlertDescription>
                {snapshotError ?? instrumentError}
              </AlertDescription>
            </Alert>
          )}
          <div className="flex flex-wrap items-center gap-3 border-t pt-6">
            <Button
              size="lg"
              onClick={() => void runStudy()}
              disabled={!ready || isRunning || snapshotLoading}
            >
              {isRunning ? <Loader2 className="animate-spin" /> : <Play />}
              {mode === 'impact'
                ? 'Measure impact'
                : mode === 'trade'
                  ? 'Run trade test'
                  : 'Measure holding risk'}
            </Button>
            {isRunning && (
              <Button
                variant="outline"
                size="lg"
                onClick={() => abortRef.current?.abort()}
              >
                Cancel
              </Button>
            )}
            <span className="text-sm text-muted-foreground">
              {snapshotLoading
                ? 'Loading verified event data…'
                : `${selectedKind[2]} · ${displaySymbol(symbol)} · ${rangePreset === 'custom' ? 'custom range' : `${rangePreset}Y`}`}
            </span>
          </div>
        </CardContent>
      </Card>
      <RunStatus run={run} />
      {run.status === 'done' && (
        <div ref={resultsRef}>
          <Results
            mode={mode}
            result={run.result}
            events={run.events}
            kindLabel={selectedKind[2]}
            symbol={symbol}
            horizon={horizon}
            timeframe={timeframe}
            ruleSentence={ruleSentence}
          />
        </div>
      )}
    </div>
  )
}

function RunStatus({ run }: { run: RunPhase }) {
  if (run.status === 'idle' || run.status === 'done') return null
  if (run.status === 'preparing')
    return (
      <Card>
        <CardContent className="flex items-center gap-4 py-5">
          <Database className="size-5 text-primary" />
          <div className="min-w-0 flex-1">
            <div className="mb-2 flex justify-between text-sm">
              <span>{run.stage}</span>
              <span>{run.percent}%</span>
            </div>
            <Progress value={run.percent} />
          </div>
        </CardContent>
      </Card>
    )
  if (run.status === 'studying')
    return (
      <Alert>
        <Loader2 className="size-4 animate-spin" />
        <AlertTitle>Running every historical release</AlertTitle>
        <AlertDescription>
          Aligning releases, measuring reactions, and applying the rule without
          borrowing future information.
        </AlertDescription>
      </Alert>
    )
  if (run.status === 'empty')
    return (
      <Alert>
        <Info className="size-4" />
        <AlertTitle>No matching releases</AlertTitle>
        <AlertDescription>
          Try a longer range or a different event dataset.
        </AlertDescription>
      </Alert>
    )
  if (run.status === 'cancelled')
    return (
      <Alert>
        <Info className="size-4" />
        <AlertTitle>Test cancelled</AlertTitle>
      </Alert>
    )
  if (run.status === 'unsupported')
    return (
      <Alert>
        <Info className="size-4" />
        <AlertTitle>Update the local Assay service</AlertTitle>
        <AlertDescription>{run.message}</AlertDescription>
      </Alert>
    )
  if (run.status === 'rule-unavailable')
    return (
      <Alert>
        <Info className="size-4" />
        <AlertTitle>This direction rule needs forecast data</AlertTitle>
        <AlertDescription>{run.message}</AlertDescription>
      </Alert>
    )
  if (run.status !== 'error') return null
  return (
    <Alert variant="destructive">
      <XCircle className="size-4" />
      <AlertTitle>
        The {displayTimeframe(run.timeframe)} test for{' '}
        {displaySymbol(run.symbol)} could not finish
      </AlertTitle>
      <AlertDescription>{friendlyError(run.message)}</AlertDescription>
    </Alert>
  )
}

function Results({
  mode,
  result,
  events,
  kindLabel,
  symbol,
  horizon,
  timeframe,
  ruleSentence,
}: {
  mode: Mode
  result: EventStudyResponse
  events: EventRow[]
  kindLabel: string
  symbol: string
  horizon: string
  timeframe: string
  ruleSentence: string
}) {
  const focus = result.windows.find((item) => item.window === horizon)
  const eventMap = new Map(events.map((item) => [item.id, item]))
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">
            {mode === 'impact'
              ? `${kindLabel} market impact`
              : mode === 'trade'
                ? `${kindLabel} trade result`
                : `${kindLabel} holding risk`}
          </h2>
          <p className="mt-1 text-sm text-muted-foreground">
            {displaySymbol(symbol)} · {displayTimeframe(timeframe)} bars ·{' '}
            {ruleSentence}
          </p>
        </div>
        <Badge variant="secondary">
          {result.eligibleEvents.length} usable releases
        </Badge>
      </div>
      {mode === 'impact' ? (
        <ImpactSummary focus={focus} />
      ) : (
        <TradeSummary
          mode={mode}
          summary={result.tradeSummary}
          trades={result.trades}
        />
      )}
      {mode === 'impact' ? (
        <ImpactTable windows={result.windows} focus={horizon} />
      ) : (
        <TradesTable trades={result.trades} events={eventMap} />
      )}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Evidence and limitations</CardTitle>
          <CardDescription>
            Enough detail to reproduce the answer and understand what it does
            not claim.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 text-sm sm:grid-cols-2 lg:grid-cols-4">
          <Audit
            label="Event snapshot"
            value={shortHash(result.eventSnapshot)}
          />
          <Audit label="Price snapshot" value={shortHash(result.barSnapshot)} />
          <Audit label="Method" value={result.methodologyVersion} />
          <Audit
            label="Excluded"
            value={String(result.excludedEvents.length)}
          />
          <div className="rounded-lg bg-muted/40 p-3 text-muted-foreground sm:col-span-2 lg:col-span-4">
            Historical estimate, not causal proof or a forecast. Entries occur
            only after the release is knowable. Costs are deducted. If one OHLC
            bar hits both stop and target, the stop is assumed first.
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function ImpactSummary({ focus }: { focus?: EventWindowSummary }) {
  if (!focus)
    return (
      <Alert>
        <Info className="size-4" />
        <AlertTitle>No complete reaction window</AlertTitle>
      </Alert>
    )
  const directional = focus.count
    ? Math.max(focus.upCount, focus.downCount) / focus.count
    : 0
  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <Metric
        title="Typical absolute move"
        value={percent(focus.medianAbs, false)}
        note={`Median after ${focus.window.toUpperCase()}`}
        icon={BarChart3}
      />
      <Metric
        title="Versus ordinary weeks"
        value={focus.impactRatio ? `${focus.impactRatio.toFixed(1)}×` : '—'}
        note="Same clock-time baseline"
        icon={Activity}
      />
      <Metric
        title="Directional consistency"
        value={percent(directional, false)}
        note={`${focus.upCount} up · ${focus.downCount} down`}
        icon={TrendingUp}
      />
      <Metric
        title="Releases measured"
        value={String(focus.count)}
        note="Complete windows only"
        icon={CheckCircle2}
      />
    </div>
  )
}

function TradeSummary({
  mode,
  summary,
  trades,
}: {
  mode: Mode
  summary?: EventTradeSummary
  trades: EventTrade[]
}) {
  if (!summary || !summary.trades)
    return (
      <Alert>
        <Info className="size-4" />
        <AlertTitle>No releases passed this rule</AlertTitle>
        <AlertDescription>
          Broaden the surprise filter, range, or entry rule.
        </AlertDescription>
      </Alert>
    )
  const worst = trades.reduce<EventTrade | undefined>(
    (current, trade) =>
      !current || trade.netPnl < current.netPnl ? trade : current,
    undefined,
  )
  if (mode === 'risk')
    return (
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Metric
          title="Chance of a loss"
          value={percent(summary.losses / summary.trades, false)}
          note={`${summary.losses} of ${summary.trades} releases`}
          icon={TrendingDown}
          tone="bad"
        />
        <Metric
          title="Worst release"
          value={worst ? signedMoneyCents(worst.netPnl) : '—'}
          note={worst ? formatMs(worst.eventTimeMs) : 'No complete trades'}
          icon={ShieldAlert}
          tone="bad"
        />
        <Metric
          title="Average outcome"
          value={signedMoneyCents(summary.expectancy)}
          note="At selected exposure"
          icon={CircleDollarSign}
        />
        <Metric
          title="Average adverse move"
          value={percent(summary.averageMae, false)}
          note="Before exit"
          icon={Activity}
        />
      </div>
    )
  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <Metric
        title="Net result"
        value={signedMoneyCents(summary.netPnl)}
        note={`${formatMoneyCents(summary.costs)} total costs`}
        icon={summary.netPnl >= 0n ? TrendingUp : TrendingDown}
        tone={summary.netPnl >= 0n ? 'good' : 'bad'}
      />
      <Metric
        title="Win rate"
        value={percent(summary.wins / summary.trades, false)}
        note={`${summary.wins} wins · ${summary.losses} losses`}
        icon={CheckCircle2}
      />
      <Metric
        title="Average per release"
        value={signedMoneyCents(summary.expectancy)}
        note={`${summary.trades} trades`}
        icon={CircleDollarSign}
      />
      <Metric
        title="Maximum drawdown"
        value={formatMoneyCents(summary.maxDrawdown)}
        note={`${summary.whipsaws} whipsaw releases`}
        icon={ShieldAlert}
        tone="bad"
      />
    </div>
  )
}

function ImpactTable({
  windows,
  focus,
}: {
  windows: EventWindowSummary[]
  focus: string
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Reaction by horizon</CardTitle>
        <CardDescription>
          Absolute movement shows magnitude; signed movement shows direction.
        </CardDescription>
      </CardHeader>
      <CardContent className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>After release</TableHead>
              <TableHead>Typical move</TableHead>
              <TableHead>Average signed</TableHead>
              <TableHead>Up / down</TableHead>
              <TableHead>Baseline</TableHead>
              <TableHead>Sample</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {windows.map((item) => (
              <TableRow
                key={item.window}
                className={item.window === focus ? 'bg-primary/5' : ''}
              >
                <TableCell className="font-medium">
                  {HORIZONS.find((h) => h[0] === item.window)?.[1] ??
                    item.window}
                </TableCell>
                <TableCell>{percent(item.medianAbs, false)}</TableCell>
                <TableCell>{percent(item.mean)}</TableCell>
                <TableCell>
                  {item.upCount} / {item.downCount}
                </TableCell>
                <TableCell>
                  {item.impactRatio ? `${item.impactRatio.toFixed(1)}×` : '—'}
                </TableCell>
                <TableCell>{item.count}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function TradesTable({
  trades,
  events,
}: {
  trades: EventTrade[]
  events: Map<string, EventRow>
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Every simulated release</CardTitle>
        <CardDescription>
          Inspect the sample instead of trusting only the headline.
        </CardDescription>
      </CardHeader>
      <CardContent className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Release</TableHead>
              <TableHead>Actual / forecast</TableHead>
              <TableHead>Entry</TableHead>
              <TableHead>Side</TableHead>
              <TableHead>Exit</TableHead>
              <TableHead>Net P&amp;L</TableHead>
              <TableHead>Best / worst</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {trades.map((trade) => {
              const event = events.get(trade.eventId)
              return (
                <TableRow key={`${trade.eventId}-${trade.entryTimeMs}`}>
                  <TableCell>
                    <div className="font-medium">
                      {formatMs(trade.eventTimeMs)}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      {event?.title || trade.eventId}
                    </div>
                  </TableCell>
                  <TableCell>
                    {event?.actual || '—'} / {event?.forecast || '—'}
                    {trade.hasSurprise && (
                      <div className="text-xs text-muted-foreground">
                        {trade.surpriseZ.toFixed(2)}σ surprise
                      </div>
                    )}
                  </TableCell>
                  <TableCell>{formatMs(trade.entryTimeMs)}</TableCell>
                  <TableCell className="capitalize">{trade.side}</TableCell>
                  <TableCell className="capitalize">
                    {trade.exitReason}
                    {trade.ambiguous && (
                      <Badge variant="outline" className="ml-2">
                        stop first
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell
                    className={cn(
                      'font-medium',
                      trade.netPnl >= 0n ? 'text-success' : 'text-destructive',
                    )}
                  >
                    {signedMoneyCents(trade.netPnl)}
                  </TableCell>
                  <TableCell>
                    {percent(trade.mfe)} / {percent(trade.mae)}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function Section({
  number,
  title,
  children,
}: {
  number: string
  title: string
  children: React.ReactNode
}) {
  return (
    <section className="space-y-4 border-b pb-7 last:border-0 last:pb-0">
      <div className="flex items-center gap-3">
        <span className="grid size-7 place-items-center rounded-full bg-foreground text-xs font-semibold text-background">
          {number}
        </span>
        <h3 className="font-semibold">{title}</h3>
      </div>
      {children}
    </section>
  )
}
function Field({
  label,
  htmlFor,
  hint,
  children,
}: {
  label: string
  htmlFor: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}
function MoneyField({
  label,
  id,
  value,
  onChange,
}: {
  label: string
  id: string
  value: string
  onChange: (value: string) => void
}) {
  return (
    <Field label={label} htmlFor={id}>
      <div className="relative">
        <span className="absolute left-3 top-1/2 -translate-y-1/2 text-sm text-muted-foreground">
          $
        </span>
        <Input
          id={id}
          className="pl-7"
          inputMode="decimal"
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
      </div>
    </Field>
  )
}
function PercentField({
  label,
  id,
  value,
  onChange,
  placeholder,
}: {
  label: string
  id: string
  value: string
  onChange: (value: string) => void
  placeholder: string
}) {
  return (
    <Field label={label} htmlFor={id}>
      <div className="relative">
        <Input
          id={id}
          className="pr-8"
          inputMode="decimal"
          value={value}
          onChange={(event) => onChange(event.target.value)}
          placeholder={placeholder}
        />
        <span className="absolute right-3 top-1/2 -translate-y-1/2 text-sm text-muted-foreground">
          %
        </span>
      </div>
    </Field>
  )
}
function Metric({
  title,
  value,
  note,
  icon: Icon,
  tone,
}: {
  title: string
  value: string
  note: string
  icon: typeof Activity
  tone?: 'good' | 'bad'
}) {
  return (
    <Card>
      <CardContent className="pt-5">
        <div className="flex items-center justify-between text-sm text-muted-foreground">
          <span>{title}</span>
          <Icon className="size-4" />
        </div>
        <div
          className={cn(
            'mt-2 text-2xl font-semibold',
            tone === 'good' && 'text-success',
            tone === 'bad' && 'text-destructive',
          )}
        >
          {value}
        </div>
        <div className="mt-1 text-xs text-muted-foreground">{note}</div>
      </CardContent>
    </Card>
  )
}
function Audit({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-muted-foreground">{label}</div>
      <div className="mt-1 font-mono font-medium">{value || '—'}</div>
    </div>
  )
}
function friendlyError(message: string) {
  if (/unsupported resolution/i.test(message))
    return `${message}. Impact can use H1 or D1; trade and risk tests require M1 or M5 from the updated Dukascopy source.`
  if (/prepared locally|missing/i.test(message))
    return `${message}. Assay tried to prepare the range automatically; check the local data service and try again.`
  return message
}

import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Play,
  XCircle,
  CheckCircle2,
  AlertTriangle,
  RefreshCw,
  Download,
} from 'lucide-react'
import { PageHeader } from '@/components/page-header'
import { ConstraintsBanner } from '@/components/constraints-banner'
import { EquitySparkline } from '@/components/equity-sparkline'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { dataApi, studyApi, rpcErrorMessage } from '@/lib/rpc'
import { dataKey, hasReadyData, session, useSession } from '@/lib/store'
import { toJsonBytes, formatBytes, formatMs, formatUsdCents } from '@/lib/codec'
import { parseStrategy, toEngineStrategy } from '@/lib/strategy'
import { isTimeframeSupportedForInstrument } from '@/lib/constraints'
import { DataReadiness } from '@/gen/assay/v1/service_pb'

export const Route = createFileRoute('/run')({ component: RunScreen })

interface ProgressState {
  percent: number
  stage: string
  currentFile: string
  bytesDone: bigint
  bytesTotal: bigint
}

interface Summary {
  trades: number
  unresolved: number
  error: string
  runId: string
  wins: number
  losses: number
  longTrades: number
  shortTrades: number
  netPnl: bigint
  expectancy: number
  profitFactor: number
  errorCode: string
}

type Phase = 'idle' | 'streaming' | 'done' | 'error' | 'cancelled'
type FixAction = 'data' | 'strategy' | null

function RunScreen() {
  const navigate = useNavigate()
  const s = useSession()
  const [phase, setPhase] = useState<Phase>('idle')
  const [progress, setProgress] = useState<ProgressState | null>(null)
  const [equity, setEquity] = useState<bigint[]>([])
  const [streamedTrades, setStreamedTrades] = useState(0)
  const [summary, setSummary] = useState<Summary | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [validationErrors, setValidationErrors] = useState<string[]>([])
  const [fixing, setFixing] = useState<FixAction>(null)
  const abortRef = useRef<AbortController | null>(null)

  const timeframeIsSupported = isTimeframeSupportedForInstrument(
    s.symbol,
    s.timeframe,
  )
  // A run is refused before it starts when the document cannot name a market
  // and an entry timeframe. Discovering that from a server error after a
  // multi-minute data preparation is a waste of the user's afternoon.
  const strategyPreflight = useMemo(() => {
    const document = parseStrategy(s.strategyText)
    return (
      !!document &&
      document.universe.symbols.length > 0 &&
      !!document.timeframes.entry
    )
  }, [s.strategyText])
  const dataReady = hasReadyData(s)
  const strategyReady = !!s.validatedHash && strategyPreflight
  const canRun =
    !!s.symbol &&
    !!s.timeframe &&
    timeframeIsSupported &&
    s.fromMs !== null &&
    s.toMs !== null &&
    s.fromMs < s.toMs &&
    dataReady &&
    strategyReady &&
    s.realityConfirmed &&
    !!s.strategyText.trim()

  const recheckData = useCallback(
    async (showError = true) => {
      if (s.fromMs === null || s.toMs === null) return false
      const key = dataKey(s)
      const status = await dataApi.dataStatus({
        symbol: s.symbol,
        timeframe: s.timeframe,
        fromMs: s.fromMs,
        toMs: s.toMs,
      })
      session.setDataStatus({
        key,
        readiness:
          status.readiness === DataReadiness.READY
            ? 'ready'
            : status.readiness === DataReadiness.PARTIAL
              ? 'partial'
              : status.readiness === DataReadiness.MISSING
                ? 'missing'
                : status.readiness === DataReadiness.CORRUPT
                  ? 'corrupt'
                  : status.readiness === DataReadiness.UNSUPPORTED
                    ? 'unsupported'
                    : status.readiness === DataReadiness.SOURCE_UNAVAILABLE
                      ? 'source_unavailable'
                      : 'unknown',
        snapshot: status.snapshot,
        missingPartitions: status.missingPartitions.length,
        cachedFromMs: status.cachedFromMs === 0n ? null : status.cachedFromMs,
        cachedToMs: status.cachedToMs === 0n ? null : status.cachedToMs,
        externalAccessRequired: status.externalAccessRequired,
        errorCode: status.errorCode,
        message: status.error,
        checkedAt: Date.now(),
      })
      if (status.readiness !== DataReadiness.READY) {
        if (showError) {
          setError(
            status.error ||
              'Data must be prepared locally before this backtest can run.',
          )
          setPhase('error')
        }
        return false
      }
      return true
    },
    [s],
  )

  useEffect(() => {
    if (
      s.symbol &&
      s.timeframe &&
      timeframeIsSupported &&
      s.fromMs !== null &&
      s.toMs !== null &&
      s.fromMs < s.toMs &&
      !dataReady
    ) {
      void recheckData(false).catch((err) => setError(rpcErrorMessage(err)))
    }
  }, [
    dataReady,
    recheckData,
    s.fromMs,
    s.symbol,
    s.timeframe,
    s.toMs,
    timeframeIsSupported,
  ])

  const prepareData = useCallback(async () => {
    if (s.fromMs === null || s.toMs === null) return
    setFixing('data')
    setError(null)
    try {
      for await (const p of dataApi.ensureData({
        symbol: s.symbol,
        timeframe: s.timeframe,
        fromMs: s.fromMs,
        toMs: s.toMs,
      })) {
        setProgress({
          percent: p.percent,
          stage: p.stage,
          currentFile: p.currentFile,
          bytesDone: p.bytesDone,
          bytesTotal: p.bytesTotal,
        })
      }
      await recheckData(false)
    } catch (err) {
      setError(rpcErrorMessage(err))
      setPhase('error')
    } finally {
      setFixing(null)
    }
  }, [recheckData, s.fromMs, s.symbol, s.timeframe, s.toMs])

  const validateStrategy = useCallback(async () => {
    setFixing('strategy')
    setError(null)
    setValidationErrors([])
    const controller = new AbortController()
    const timeout = window.setTimeout(() => controller.abort(), 12_000)
    try {
      const parsed = parseStrategy(s.strategyText)
      if (!parsed) throw new Error('Strategy document isn’t valid JSON.')
      const normalized = toEngineStrategy(parsed)
      const result = await studyApi.validate(
        { strategyJson: toJsonBytes(normalized) },
        { signal: controller.signal },
      )
      const messages = result.errors.map((item) =>
        item.path ? `${item.path}: ${item.message}` : item.message,
      )
      setValidationErrors(messages)
      session.set({
        strategyText: JSON.stringify(normalized, null, 2),
        validatedHash: messages.length ? null : result.hash,
      })
    } catch (err) {
      setError(
        controller.signal.aborted
          ? 'Strategy validation took too long. Check the local Assay server and try again.'
          : rpcErrorMessage(err),
      )
      setPhase('error')
    } finally {
      window.clearTimeout(timeout)
      setFixing(null)
    }
  }, [s.strategyText, s.symbol, s.timeframe])

  const run = async () => {
    if (!canRun) return
    try {
      if (!(await recheckData())) return
    } catch (err) {
      setError(rpcErrorMessage(err))
      setPhase('error')
      return
    }
    let doc: Record<string, unknown>
    const parsed = parseStrategy(s.strategyText)
    if (!parsed) {
      // Parse failures and semantic failures are different problems with
      // different fixes. Folding the second into "isn't valid JSON" tells the
      // user their syntax is broken when it is not.
      setError('Strategy document isn’t valid JSON.')
      setPhase('error')
      return
    }
    try {
      doc = toEngineStrategy(parsed)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setPhase('error')
      return
    }
    if (s.fromMs === null || s.toMs === null) return

    const controller = new AbortController()
    abortRef.current = controller
    setPhase('streaming')
    setError(null)
    setProgress(null)
    setEquity([])
    setStreamedTrades(0)
    setSummary(null)

    try {
      let terminalError = false
      for await (const update of studyApi.run(
        {
          strategyJson: toJsonBytes(doc),
          snapshot: s.snapshot,
          fromMs: s.fromMs,
          toMs: s.toMs,
          assumptions: s.assumptions,
        },
        { signal: controller.signal },
      )) {
        switch (update.payload.case) {
          case 'progress': {
            const p = update.payload.value
            setProgress({
              percent: p.percent,
              stage: p.stage,
              currentFile: p.currentFile,
              bytesDone: p.bytesDone,
              bytesTotal: p.bytesTotal,
            })
            break
          }
          case 'equity': {
            const chunk = update.payload.value.values
            setEquity((prev) => prev.concat(chunk))
            break
          }
          case 'trades': {
            const c = update.payload.value.count
            setStreamedTrades((prev) => prev + c)
            break
          }
          case 'summary': {
            const sum = update.payload.value
            const parsed: Summary = {
              trades: sum.trades,
              unresolved: sum.unresolved,
              error: sum.error,
              runId: sum.runId,
              wins: sum.wins,
              losses: sum.losses,
              longTrades: sum.longTrades,
              shortTrades: sum.shortTrades,
              netPnl: sum.netPnl,
              expectancy: sum.expectancy,
              profitFactor: sum.profitFactor,
              errorCode: sum.errorCode,
            }
            setSummary(parsed)
            // Recording the run is what increments the disclosed trial count.
            if (sum.runId) session.recordRun(sum.runId)
            if (sum.error || sum.errorCode) {
              terminalError = true
              setError(sum.error || `The run stopped with ${sum.errorCode}.`)
              setPhase('error')
            } else {
              // The terminal summary means all result chunks have arrived. Do
              // not leave the UI at the server's last intermediate milestone
              // (currently 80%) while the transport finishes closing.
              setProgress((current) => ({
                percent: 100,
                stage: 'Run complete',
                currentFile: current?.currentFile ?? '',
                bytesDone: current?.bytesDone ?? 0n,
                bytesTotal: current?.bytesTotal ?? 0n,
              }))
              setPhase('done')
              if (sum.runId)
                void navigate({
                  to: '/runs/$runId',
                  params: { runId: sum.runId },
                })
            }
            break
          }
          default:
            break
        }
      }
      if (!terminalError) setPhase('done')
    } catch (err) {
      if (controller.signal.aborted) {
        setPhase('cancelled')
      } else {
        setError(rpcErrorMessage(err))
        setPhase('error')
      }
    } finally {
      abortRef.current = null
    }
  }

  const cancel = () => abortRef.current?.abort()

  const total = Number(progress?.bytesTotal ?? 0n)
  const bytePct =
    total > 0 ? (Number(progress?.bytesDone ?? 0n) / total) * 100 : undefined

  return (
    <div className="page-form">
      <PageHeader
        step={5}
        title="Run"
        description="Run the validated strategy using the prepared local dataset."
      />

      <div className="grid gap-6 lg:grid-cols-[minmax(0,320px)_1fr]">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle>Ready check</CardTitle>
            <CardDescription>
              Assay only runs after data and strategy are ready.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="rounded-md border border-border bg-muted/30 p-3">
              <div className="flex flex-wrap gap-2">
                {s.symbol ? (
                  <Badge variant="secondary">{s.symbol}</Badge>
                ) : null}
                {s.timeframe ? (
                  <Badge variant="outline">{s.timeframe}</Badge>
                ) : null}
              </div>
              <p className="mt-2 text-sm text-muted-foreground">
                {s.fromMs !== null && s.toMs !== null
                  ? `${formatMs(s.fromMs)} to ${formatMs(s.toMs)}`
                  : 'Choose a date window on Data.'}
              </p>
            </div>

            <PreflightChecklist
              market={!!s.symbol && !!s.timeframe && timeframeIsSupported}
              data={dataReady}
              strategy={strategyReady}
              reality={s.realityConfirmed}
              window={s.fromMs !== null && s.toMs !== null && s.fromMs < s.toMs}
              dataStatus={s.dataStatus?.readiness ?? 'unknown'}
              fixing={fixing}
              onCheckData={() =>
                void recheckData().catch((err) =>
                  setError(rpcErrorMessage(err)),
                )
              }
              onPrepareData={() => void prepareData()}
              onValidateStrategy={() => void validateStrategy()}
            />

            <div className="grid grid-cols-2 gap-2">
              <Button
                variant="outline"
                asChild
                disabled={phase === 'streaming'}
                className="w-full"
              >
                <Link to="/data">Back to prepare data</Link>
              </Button>
              <Button
                onClick={() => void run()}
                disabled={!canRun || phase === 'streaming'}
                className="w-full"
              >
                <Play className="size-4" />
                {phase === 'streaming' ? 'Running…' : 'Run backtest'}
              </Button>
              {phase === 'streaming' ? (
                <Button
                  variant="outline"
                  onClick={cancel}
                  className="col-span-2 w-full"
                >
                  Cancel
                </Button>
              ) : null}
            </div>
            {!canRun ? (
              <p className="text-xs text-muted-foreground">
                {!s.realityConfirmed
                  ? 'Review the trading reality assumptions before running.'
                  : !dataReady
                    ? 'Prepare this exact dataset on Data before running.'
                    : !strategyReady
                      ? 'Validate the strategy for the current market and timeframe before running.'
                      : !timeframeIsSupported
                        ? 'The selected timeframe is not available for this data source. Choose D1 or W1 on Data.'
                        : s.fromMs !== null &&
                            s.toMs !== null &&
                            s.fromMs >= s.toMs
                          ? 'The end date must be after the start date.'
                          : 'Choose a market, timeframe, rules, and dates before running.'}
              </p>
            ) : null}
          </CardContent>
        </Card>

        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-3">
            <Stat
              label="Balance updates"
              value={equity.length.toLocaleString()}
            />
            <Stat
              label="Trades (streamed)"
              value={streamedTrades.toLocaleString()}
            />
            <Stat
              label="Progress"
              value={phase === 'done' ? '100%' : `${progress?.percent ?? 0}%`}
            />
          </div>

          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base">Balance (USD)</CardTitle>
              <CardDescription>
                Streamed chunks, plotted relative to their own range.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <EquitySparkline values={equity} />
            </CardContent>
          </Card>

          {phase !== 'idle' ? (
            <Card>
              <CardContent className="space-y-3 pt-6">
                <div className="flex items-center justify-between text-sm">
                  <span className="font-medium">
                    {progress?.stage ||
                      (phase === 'streaming' ? 'starting…' : phase)}
                  </span>
                  {phase === 'cancelled' ? (
                    <Badge variant="warning">cancelled</Badge>
                  ) : (
                    <span className="tabular-nums text-muted-foreground">
                      {progress?.percent ?? 0}%
                    </span>
                  )}
                </div>
                <Progress
                  value={progress?.percent ?? 0}
                  indeterminate={phase === 'streaming' && !progress}
                />
                {progress?.currentFile ? (
                  <p className="truncate font-mono text-xs text-muted-foreground">
                    {progress.currentFile}
                  </p>
                ) : null}
                {total > 0 ? (
                  <p className="text-xs tabular-nums text-muted-foreground">
                    {formatBytes(progress?.bytesDone)} /{' '}
                    {formatBytes(progress?.bytesTotal)}
                    {bytePct !== undefined ? ` · ${bytePct.toFixed(0)}%` : ''}
                  </p>
                ) : null}
              </CardContent>
            </Card>
          ) : null}

          {error ? (
            <Alert variant="destructive">
              <XCircle />
              <AlertTitle>Run failed</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          {validationErrors.length ? (
            <Alert variant="warning">
              <AlertTriangle />
              <AlertTitle>Strategy needs changes</AlertTitle>
              <AlertDescription className="space-y-1">
                {validationErrors.slice(0, 4).map((item) => (
                  <p key={item}>{item}</p>
                ))}
                <Link to="/strategy" className="font-medium underline">
                  Edit strategy
                </Link>
              </AlertDescription>
            </Alert>
          ) : null}

          {summary ? <SummaryCard summary={summary} /> : null}
        </div>
      </div>

      <ConstraintsBanner />
    </div>
  )
}

function SummaryCard({ summary }: { summary: Summary }) {
  const hasError = !!summary.error
  const decidedTrades = summary.wins + summary.losses
  const winRate =
    decidedTrades > 0 ? (summary.wins / decidedTrades) * 100 : null
  const unresolvedRate =
    summary.trades > 0 ? (summary.unresolved / summary.trades) * 100 : 0
  const hasHeavyAmbiguity = summary.trades > 0 && unresolvedRate >= 25
  const looksCostDominated =
    summary.trades > 0 &&
    summary.wins === 0 &&
    summary.profitFactor === 0 &&
    summary.expectancy < 0
  const isOneSided =
    summary.trades > 0 &&
    (summary.longTrades === 0 || summary.shortTrades === 0)
  const needsReview = hasError || summary.unresolved > 0 || isOneSided
  const bias =
    summary.longTrades > 0 && summary.shortTrades === 0
      ? 'Long bias'
      : summary.shortTrades > 0 && summary.longTrades === 0
        ? 'Short bias'
        : summary.trades > 0
          ? 'Mixed bias'
          : 'No trades taken'
  const formatNumber = (value: number) =>
    Number.isFinite(value) ? formatUsdCents(value, { sign: true }) : '—'
  const formatPnL = (value: bigint) => formatUsdCents(value, { sign: true })
  return (
    <Card
      className={
        hasError
          ? 'border-destructive/50'
          : needsReview
            ? 'border-warning/50'
            : 'border-success/50'
      }
    >
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-base">
          {hasError || needsReview ? (
            <AlertTriangle className="size-4 text-destructive" />
          ) : (
            <CheckCircle2 className="size-4 text-success" />
          )}
          {needsReview ? 'Run needs review' : 'Terminal summary'}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {hasError ? (
          <Alert variant="destructive">
            <XCircle />
            <AlertDescription className="space-y-2">
              <p>{summary.error}</p>
              {summary.errorCode === 'DATA_MISSING' ||
              summary.errorCode === 'DATA_PARTIAL' ? (
                <Link to="/data" className="font-medium underline">
                  Prepare data
                </Link>
              ) : null}
              {summary.error.startsWith('Strategy needs attention:') ? (
                <Link to="/strategy" className="font-medium underline">
                  Open Strategy Editor
                </Link>
              ) : null}
            </AlertDescription>
          </Alert>
        ) : null}
        {!hasError && needsReview ? (
          <Alert variant="warning">
            <AlertTriangle />
            <AlertTitle>
              Don’t treat this as a performance result yet
            </AlertTitle>
            <AlertDescription className="space-y-2">
              {summary.unresolved > 0 ? (
                <p>
                  {summary.unresolved.toLocaleString()} of{' '}
                  {summary.trades.toLocaleString()} positions (
                  {unresolvedRate.toFixed(0)}%) touched an ambiguous stop and
                  target within the same candle. The server uses the
                  conservative outcome because finer intrabar data was not
                  available to establish which exit occurred first.
                </p>
              ) : null}
              {looksCostDominated ? (
                <p>
                  The exit distances also look smaller than the effective
                  execution cost. On BTC, a 10/30 pip setup is only a $0.10 stop
                  and $0.30 target when one pip is one tick, so fees and spread
                  can turn even target exits into net losses. Use percentage
                  exits for crypto unless you intentionally want raw tick
                  distances.
                </p>
              ) : null}
              {isOneSided ? (
                <p>
                  Every entry was {summary.longTrades > 0 ? 'long' : 'short'}.
                  Add and configure the opposite direction only if the strategy
                  is meant to trade both ways.
                </p>
              ) : null}
              <Link to="/strategy" className="font-medium underline">
                Review entry and exit rules
              </Link>
            </AlertDescription>
          </Alert>
        ) : null}
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <Stat label="Trades" value={summary.trades.toLocaleString()} />
          <Stat
            label={hasHeavyAmbiguity ? 'Conservative win rate' : 'Win rate'}
            value={winRate === null ? '—' : `${winRate.toFixed(1)}%`}
            tone={winRate !== null && winRate < 50 ? 'warning' : undefined}
          />
          <Stat
            label="Wins / losses"
            value={`${summary.wins} / ${summary.losses}`}
          />
          <Stat
            label="Unresolved"
            value={summary.unresolved.toLocaleString()}
            tone={summary.unresolved > 0 ? 'warning' : undefined}
          />
        </div>
        {!hasError ? (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <Stat
              label={needsReview ? 'Net P&L (provisional)' : 'Net P&L'}
              value={formatPnL(summary.netPnl)}
              tone={summary.netPnl < 0n ? 'warning' : undefined}
            />
            <Stat
              label="Expectancy / trade (USD)"
              value={formatNumber(summary.expectancy)}
            />
            <Stat
              label="Profit factor"
              value={formatNumber(summary.profitFactor)}
            />
            <Stat
              label="Strategy bias"
              value={bias}
              detail={`${summary.longTrades} long · ${summary.shortTrades} short`}
            />
          </div>
        ) : null}
        {!hasError ? (
          <p className="text-xs text-muted-foreground">
            {looksCostDominated
              ? 'Use percentage-based BTC exits or a cost model that matches the venue/account before reading this as strategy performance.'
              : needsReview
                ? 'These figures are provisional because some candles could not establish whether the stop or target was reached first.'
                : 'Wins, losses, P&L, and expectancy use the conservative outcome when an intrabar exit is ambiguous.'}
          </p>
        ) : null}
        <div className="rounded-md border border-border bg-muted/30 px-3 py-2">
          <p className="text-xs text-muted-foreground">Run id</p>
          <code className="break-all font-mono text-sm">
            {summary.runId || '—'}
          </code>
        </div>
        {summary.runId ? (
          <Button asChild variant="outline" size="sm">
            <Link to="/history">Open in History →</Link>
          </Button>
        ) : null}
      </CardContent>
    </Card>
  )
}

function PreflightChecklist({
  market,
  data,
  strategy,
  reality,
  window,
  dataStatus,
  fixing,
  onCheckData,
  onPrepareData,
  onValidateStrategy,
}: {
  market: boolean
  data: boolean
  strategy: boolean
  reality: boolean
  window: boolean
  dataStatus: string
  fixing: FixAction
  onCheckData: () => void
  onPrepareData: () => void
  onValidateStrategy: () => void
}) {
  const dataAction =
    dataStatus === 'missing' || dataStatus === 'partial'
      ? {
          label: fixing === 'data' ? 'Preparing…' : 'Prepare data',
          icon: Download,
          onClick: onPrepareData,
        }
      : {
          label: fixing === 'data' ? 'Checking…' : 'Check data',
          icon: RefreshCw,
          onClick: onCheckData,
        }
  const items: Array<{
    label: string
    ok: boolean
    action: React.ReactNode
  }> = [
    {
      label: reality ? 'Trading reality reviewed' : 'Review trading reality',
      ok: reality,
      action: (
        <Button asChild size="sm" variant="outline">
          <Link to="/reality">Review assumptions</Link>
        </Button>
      ),
    },
    {
      label: 'Choose market and dates',
      ok: market && window,
      action: (
        <Button asChild size="sm" variant="outline">
          <Link to="/data">Edit setup</Link>
        </Button>
      ),
    },
    {
      label: data ? 'Data ready' : 'Prepare local data',
      ok: data,
      action: (
        <Button
          size="sm"
          variant="outline"
          onClick={dataAction.onClick}
          disabled={fixing !== null || !market || !window}
        >
          <dataAction.icon className="size-4" />
          {dataAction.label}
        </Button>
      ),
    },
    {
      label: strategy ? 'Strategy ready' : 'Validate strategy',
      ok: strategy,
      action: (
        <Button
          size="sm"
          variant="outline"
          onClick={onValidateStrategy}
          disabled={fixing !== null || !market}
        >
          <CheckCircle2 className="size-4" />
          {fixing === 'strategy' ? 'Validating…' : 'Validate'}
        </Button>
      ),
    },
  ]
  const missing = items.filter((item) => !item.ok)
  if (missing.length === 0) {
    return (
      <Alert className="border-success/40 bg-success/10">
        <CheckCircle2 className="size-4 text-success" />
        <AlertTitle>Ready to run</AlertTitle>
        <AlertDescription>
          Data and strategy are both ready for this setup.
        </AlertDescription>
      </Alert>
    )
  }
  return (
    <Alert variant="warning">
      <AlertTriangle className="size-4" />
      <AlertTitle>Before you run</AlertTitle>
      <AlertDescription className="space-y-2">
        {missing.map((item) => (
          <div
            key={item.label}
            className="flex items-center justify-between gap-3"
          >
            <span className="flex items-center gap-2">{item.label}</span>
            {item.action}
          </div>
        ))}
      </AlertDescription>
    </Alert>
  )
}

function Stat({
  label,
  value,
  tone,
  detail,
}: {
  label: string
  value: string
  tone?: 'warning'
  detail?: string
}) {
  return (
    <div className="rounded-lg border border-border bg-card p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p
        className={
          'mt-0.5 text-xl font-semibold tabular-nums' +
          (tone === 'warning' ? ' text-warning' : '')
        }
      >
        {value}
      </p>
      {detail ? (
        <p className="mt-0.5 text-xs text-muted-foreground">{detail}</p>
      ) : null}
    </div>
  )
}

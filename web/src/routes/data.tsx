import { createFileRoute, Link } from '@tanstack/react-router'
import { useCallback, useEffect, useRef, useState } from 'react'
import {
  RefreshCw,
  Download,
  XCircle,
  CheckCircle2,
  ArrowRight,
  AlertTriangle,
} from 'lucide-react'
import { PageHeader } from '@/components/page-header'
import { ConstraintsBanner } from '@/components/constraints-banner'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { DateField } from '@/components/date-field'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { dataApi, rpcErrorMessage } from '@/lib/rpc'
import { dataKey, hasReadyData, session, useSession } from '@/lib/store'
import type { DataReadinessState, DataStatusState } from '@/lib/store'
import { formatBytes, formatMs } from '@/lib/codec'
import {
  isTimeframeSupportedForInstrument,
  sortTimeframes,
  supportedTimeframesForInstrument,
} from '@/lib/constraints'
import { cn } from '@/lib/utils'
import { DataReadiness } from '@/gen/assay/v1/service_pb'

export const Route = createFileRoute('/data')({ component: DataScreen })

interface InstrumentRow {
  symbol: string
  qualityTier: string
  timeframes: string[]
}

interface ProgressState {
  percent: number
  stage: string
  currentFile: string
  bytesDone: bigint
  bytesTotal: bigint
}

type Phase = 'idle' | 'streaming' | 'done' | 'error' | 'cancelled'

/**
 * The windows a person actually asks for. Hand-picking two dates to get "the
 * last three years" is busywork, and a blank window makes the step look
 * unfinished. The presets end at today's UTC midnight and run backwards.
 */
const PRESET_YEARS = [1, 3, 5, 10] as const

function presetWindow(years: number): { fromMs: bigint; toMs: bigint } {
  const now = new Date()
  return {
    fromMs: BigInt(
      Date.UTC(
        now.getUTCFullYear() - years,
        now.getUTCMonth(),
        now.getUTCDate(),
      ),
    ),
    toMs: BigInt(
      Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()),
    ),
  }
}

function matchesPreset(
  fromMs: bigint | null,
  toMs: bigint | null,
  years: number,
): boolean {
  if (fromMs === null || toMs === null) return false
  const window = presetWindow(years)
  return window.fromMs === fromMs && window.toMs === toMs
}

function DataScreen() {
  const s = useSession()
  const [loading, setLoading] = useState(false)
  const [listError, setListError] = useState<string | null>(null)
  const [restoreNotice, setRestoreNotice] = useState<string | null>(null)

  const [defaultedWindow, setDefaultedWindow] = useState(false)
  const [phase, setPhase] = useState<Phase>('idle')
  const [progress, setProgress] = useState<ProgressState | null>(null)
  const [ensureError, setEnsureError] = useState<string | null>(null)
  const [updates, setUpdates] = useState(0)
  const [elapsed, setElapsed] = useState(0)
  const [lastUpdateAt, setLastUpdateAt] = useState<number | null>(null)
  const [checkingStatus, setCheckingStatus] = useState(false)
  const abortRef = useRef<AbortController | null>(null)
  const startRef = useRef(0)

  // Tick a live elapsed counter while a stream is in flight, so a stalled
  // download is visibly stalled rather than ambiguously idle.
  useEffect(() => {
    if (phase !== 'streaming') return
    const id = setInterval(() => {
      setElapsed((Date.now() - startRef.current) / 1000)
    }, 500)
    return () => clearInterval(id)
  }, [phase])

  const loadInstruments = useCallback(async () => {
    setLoading(true)
    setListError(null)
    try {
      const res = await dataApi.listInstruments({})
      const rows: InstrumentRow[] = res.instruments.map((i) => ({
        symbol: i.symbol,
        qualityTier: i.qualityTier,
        timeframes: sortTimeframes(i.timeframes),
      }))
      if (s.symbol) {
        const restored = rows.find((row) => row.symbol === s.symbol)
        if (!restored) {
          setRestoreNotice(
            'This market is no longer advertised by the server. Return to the rules step to choose an available market.',
          )
        } else {
          const supported = supportedTimeframesForInstrument(
            restored.symbol,
            restored.timeframes,
          )
          if (!supported.includes(s.timeframe)) {
            setRestoreNotice(
              `${s.timeframe} is no longer available for ${restored.symbol}. Return to the rules step to choose an available timeframe.`,
            )
          } else {
            setRestoreNotice(null)
          }
        }
      }
    } catch (err) {
      setListError(rpcErrorMessage(err))
    } finally {
      setLoading(false)
    }
  }, [s.symbol, s.timeframe])

  useEffect(() => {
    void loadInstruments()
  }, [loadInstruments])

  // A chosen market with no window yet opens on a useful default rather than
  // two empty fields. The person can still change either date.
  useEffect(() => {
    if (defaultedWindow) return
    if (s.fromMs === null && s.toMs === null && s.symbol && s.timeframe) {
      setDefaultedWindow(true)
      const window = presetWindow(3)
      session.set({ fromMs: window.fromMs, toMs: window.toMs })
    }
  }, [defaultedWindow, s.fromMs, s.toMs, s.symbol, s.timeframe])

  const selectedTimeframeIsSupported = isTimeframeSupportedForInstrument(
    s.symbol,
    s.timeframe,
  )

  const canPrepare =
    !!s.symbol &&
    !!s.timeframe &&
    selectedTimeframeIsSupported &&
    s.fromMs !== null &&
    s.toMs !== null &&
    s.fromMs < s.toMs
  const ready = hasReadyData(s)
  const currentDataKey = dataKey(s)

  const refreshDataStatus = useCallback(async (): Promise<boolean> => {
    if (!canPrepare) return false
    setCheckingStatus(true)
    try {
      const status = await dataApi.dataStatus({
        symbol: s.symbol,
        timeframe: s.timeframe,
        fromMs: s.fromMs!,
        toMs: s.toMs!,
      })
      const readiness = mapReadiness(status.readiness)
      session.setDataStatus({
        key: currentDataKey,
        readiness,
        snapshot: status.snapshot,
        missingPartitions: status.missingPartitions.length,
        cachedFromMs: status.cachedFromMs === 0n ? null : status.cachedFromMs,
        cachedToMs: status.cachedToMs === 0n ? null : status.cachedToMs,
        externalAccessRequired: status.externalAccessRequired,
        errorCode: status.errorCode,
        message: status.error,
        checkedAt: Date.now(),
      })
      return readiness === 'ready'
    } catch (err) {
      session.setDataStatus({
        key: currentDataKey,
        readiness: 'error',
        snapshot: '',
        missingPartitions: 0,
        cachedFromMs: null,
        cachedToMs: null,
        externalAccessRequired: true,
        errorCode: 'STATUS_UNAVAILABLE',
        message: rpcErrorMessage(err),
        checkedAt: Date.now(),
      })
      return false
    } finally {
      setCheckingStatus(false)
    }
  }, [canPrepare, currentDataKey, s.fromMs, s.symbol, s.timeframe, s.toMs])

  useEffect(() => {
    if (!canPrepare) return
    void refreshDataStatus()
  }, [canPrepare, refreshDataStatus])

  const runEnsure = async () => {
    if (!canPrepare) return
    const controller = new AbortController()
    abortRef.current = controller
    setPhase('streaming')
    setEnsureError(null)
    setProgress(null)
    setUpdates(0)
    setElapsed(0)
    setLastUpdateAt(null)
    startRef.current = Date.now()
    let receivedProgress = false
    try {
      for await (const p of dataApi.ensureData(
        {
          symbol: s.symbol,
          timeframe: s.timeframe,
          fromMs: s.fromMs!,
          toMs: s.toMs!,
        },
        { signal: controller.signal },
      )) {
        receivedProgress = true
        setUpdates((n) => n + 1)
        setLastUpdateAt(Date.now())
        setProgress({
          percent: p.percent,
          stage: p.stage,
          currentFile: p.currentFile,
          bytesDone: p.bytesDone,
          bytesTotal: p.bytesTotal,
        })
      }
      setElapsed((Date.now() - startRef.current) / 1000)
      if (receivedProgress) {
        // A stream may close immediately before the cache manifest is visible
        // to DataStatus. Poll the server briefly so a successful preparation
        // unlocks the next step without a stale manual refresh.
        let confirmed = false
        for (const delay of [0, 250, 750]) {
          if (delay) {
            await new Promise<void>((resolve) =>
              window.setTimeout(resolve, delay),
            )
          }
          if (await refreshDataStatus()) {
            confirmed = true
            break
          }
        }
        if (confirmed) {
          setPhase('done')
        } else {
          setEnsureError(
            'Preparation ended without saving usable local data for this exact window. Choose an earlier, fully completed historical range and retry; the run remains locked until the server confirms cached data.',
          )
          setPhase('error')
        }
      } else {
        setEnsureError(
          'The server closed the preparation stream without sending a progress update. Data may not have been prepared; try again or check the Assay server logs.',
        )
        setPhase('error')
      }
    } catch (err) {
      if (controller.signal.aborted) {
        setPhase('cancelled')
      } else {
        setEnsureError(rpcErrorMessage(err))
        setPhase('error')
      }
    } finally {
      abortRef.current = null
    }
  }

  const cancel = () => abortRef.current?.abort()

  return (
    <div className="page-form">
      <PageHeader
        step={4}
        title="Prepare the history"
        description="Choose the test period and prepare the exact local history for the rules you have already defined."
        actions={
          <Button
            variant="outline"
            size="sm"
            onClick={() => void loadInstruments()}
            disabled={loading}
          >
            <RefreshCw className={cn('size-4', loading && 'animate-spin')} />
            Refresh
          </Button>
        }
      />

      {listError ? (
        <Alert variant="destructive">
          <XCircle />
          <AlertTitle>Couldn’t reach the Assay server</AlertTitle>
          <AlertDescription>
            {listError}. Make sure <code>assay</code> is running and reachable
            at the configured base URL (default proxied to 127.0.0.1:8080).
          </AlertDescription>
        </Alert>
      ) : null}

      {restoreNotice ? (
        <Alert variant="info">
          <AlertTitle>Restored setup adjusted</AlertTitle>
          <AlertDescription>{restoreNotice}</AlertDescription>
        </Alert>
      ) : null}

      <div className="max-w-2xl">
        <Card>
          <CardHeader>
            <CardTitle>Prepare data</CardTitle>
            <CardDescription>
              Dates are read as UTC. The end date is not included in the test.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-muted/30 p-3">
              <div>
                <p className="runlog-label">Testing</p>
                <p className="mt-1 text-sm font-medium">
                  {s.symbol && s.timeframe
                    ? `${s.symbol} · ${s.timeframe}`
                    : 'No market selected'}
                </p>
              </div>
              <Button variant="outline" size="sm" asChild>
                <Link to="/strategy">Change rules</Link>
              </Button>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs text-muted-foreground">Quick range</span>
              {PRESET_YEARS.map((years) => (
                <Button
                  key={years}
                  type="button"
                  size="sm"
                  variant={
                    matchesPreset(s.fromMs, s.toMs, years)
                      ? 'default'
                      : 'outline'
                  }
                  onClick={() => {
                    const window = presetWindow(years)
                    session.set({ fromMs: window.fromMs, toMs: window.toMs })
                  }}
                >
                  {years}Y
                </Button>
              ))}
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="from">From (UTC)</Label>
                <DateField
                  id="from"
                  value={s.fromMs ?? null}
                  onChange={(fromMs) => session.set({ fromMs })}
                  placeholder="Start of window"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="to">To (UTC, exclusive)</Label>
                <DateField
                  id="to"
                  value={s.toMs ?? null}
                  onChange={(toMs) => session.set({ toMs })}
                  placeholder="End of window"
                />
              </div>
            </div>

            <div className="flex flex-col gap-2 min-[420px]:flex-row">
              <Button
                onClick={() => void runEnsure()}
                disabled={!canPrepare || phase === 'streaming' || ready}
              >
                {ready ? (
                  <CheckCircle2 className="size-4" />
                ) : phase === 'error' ? (
                  <RefreshCw className="size-4" />
                ) : (
                  <Download className="size-4" />
                )}
                {ready
                  ? 'Ready locally'
                  : phase === 'streaming'
                    ? 'Preparing…'
                    : phase === 'error'
                      ? 'Retry preparation'
                      : 'Prepare data'}
              </Button>
              {phase === 'streaming' ? (
                <Button variant="outline" onClick={cancel}>
                  Cancel
                </Button>
              ) : null}
            </div>

            {!canPrepare ? (
              <p className="text-xs text-muted-foreground">
                {s.fromMs !== null && s.toMs !== null && s.fromMs >= s.toMs
                  ? 'The end date must be after the start date.'
                  : !selectedTimeframeIsSupported
                    ? 'Choose a supported market and timeframe in the rules step.'
                    : 'Choose both dates to enable preparation.'}
              </p>
            ) : null}

            <ReadinessPanel
              canCheck={canPrepare}
              status={s.dataStatus}
              currentKey={currentDataKey}
              onRefresh={() => void refreshDataStatus()}
              checkingStatus={checkingStatus}
            />

            <ProgressPanel
              phase={phase}
              progress={progress}
              error={ensureError}
              updates={updates}
              elapsed={elapsed}
              lastUpdateAt={lastUpdateAt}
              onRetry={() => void runEnsure()}
            />

            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
              <Button variant="outline" asChild>
                <Link to="/reality">Back to trading reality</Link>
              </Button>
              {canPrepare && ready ? (
                <div className="flex flex-wrap items-center justify-end gap-3">
                  <span className="text-xs text-muted-foreground">
                    This exact setup is ready locally.
                  </span>
                  <Button asChild>
                    <Link to="/run">
                      Continue to run
                      <ArrowRight className="size-4" />
                    </Link>
                  </Button>
                </div>
              ) : (
                <span className="text-xs text-muted-foreground">
                  Prepare this date window to continue.
                </span>
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      <ConstraintsBanner />
    </div>
  )
}

function mapReadiness(readiness: DataReadiness): DataReadinessState {
  switch (readiness) {
    case DataReadiness.READY:
      return 'ready'
    case DataReadiness.PARTIAL:
      return 'partial'
    case DataReadiness.MISSING:
      return 'missing'
    case DataReadiness.SOURCE_UNAVAILABLE:
      return 'source_unavailable'
    case DataReadiness.CORRUPT:
      return 'corrupt'
    case DataReadiness.UNSUPPORTED:
      return 'unsupported'
    default:
      return 'unknown'
  }
}

function ReadinessPanel({
  canCheck,
  status,
  currentKey,
  onRefresh,
  checkingStatus,
}: {
  canCheck: boolean
  status: DataStatusState | null
  currentKey: string
  onRefresh: () => void
  checkingStatus: boolean
}) {
  if (!canCheck) return null
  if (!status || status.key !== currentKey) {
    return (
      <Alert variant="info">
        <RefreshCw className="size-4" />
        <AlertTitle>Checking local data</AlertTitle>
        <AlertDescription>
          Assay is checking whether this exact market and period are ready.
        </AlertDescription>
      </Alert>
    )
  }

  if (status.readiness === 'ready') {
    return (
      <Alert className="border-success/40 bg-success/10">
        <CheckCircle2 className="size-4" />
        <AlertTitle>Ready locally</AlertTitle>
        <AlertDescription>
          This market and period are ready through{' '}
          {formatMs(status.cachedToMs ?? undefined)}.
        </AlertDescription>
      </Alert>
    )
  }

  const needsPrepare =
    status.readiness === 'missing' || status.readiness === 'partial'
  return (
    <Alert variant={needsPrepare ? 'warning' : 'destructive'}>
      <AlertTriangle className="size-4" />
      <AlertTitle>
        {needsPrepare ? 'Data required' : 'Data status needs attention'}
      </AlertTitle>
      <AlertDescription className="space-y-2">
        <p>
          {needsPrepare
            ? 'This market and period need to be prepared before you can run the test.'
            : status.message ||
              status.errorCode ||
              'The server could not confirm local readiness.'}
        </p>
        <Button
          size="sm"
          variant="outline"
          onClick={onRefresh}
          disabled={checkingStatus}
        >
          <RefreshCw
            className={cn('size-4', checkingStatus && 'animate-spin')}
          />
          {checkingStatus ? 'Rechecking…' : 'Recheck'}
        </Button>
      </AlertDescription>
    </Alert>
  )
}

function ProgressPanel({
  phase,
  progress,
  error,
  updates,
  elapsed,
  lastUpdateAt,
  onRetry,
}: {
  phase: Phase
  progress: ProgressState | null
  error: string | null
  updates: number
  elapsed: number
  lastUpdateAt: number | null
  onRetry: () => void
}) {
  if (phase === 'idle') return null

  if (phase === 'error') {
    const msg = error ?? ''
    const dukascopyIntegrityFailure =
      /dukascopy: normalised invalid series/i.test(msg)
    const dukascopyFailure = /dukascopy:/i.test(msg)
    const unavailableBinanceArchive =
      /venue: unavailable: binance has no .* bars for .* in \d{4}-\d{2}/i.test(
        msg,
      )
    const looksLikeReach =
      /deadline_exceeded|i\/o timeout|lookup|dial tcp|no such host|connection refused/i.test(
        msg,
      )
    return (
      <Alert variant="destructive">
        <XCircle />
        <AlertTitle>Preparation failed</AlertTitle>
        <AlertDescription className="space-y-3">
          {dukascopyIntegrityFailure ? (
            <p>
              The source responded, but its candle payload failed Assay’s data
              integrity checks. No data was saved locally. This is a parser
              issue rather than a download or Cloudflare issue.
            </p>
          ) : dukascopyFailure ? (
            <p>
              Dukascopy’s historical file server is unavailable. No H1/H4 data
              was saved. You can retry, or use D1/W1 while the source is down.
            </p>
          ) : unavailableBinanceArchive ? (
            <p>
              Binance is reachable, but it has not published the requested
              month’s archive for this market and timeframe. The homepage being
              online does not guarantee that every historical ZIP exists. Pick
              an earlier completed period; retrying the same month will not add
              bars until Binance publishes them.
            </p>
          ) : looksLikeReach ? (
            <p>
              The server couldn’t reach the data source. This is a server-side
              network error, not a UI one — the fallback tried to fetch from the
              public venue and timed out. Check the machine’s connectivity/DNS,
              or run <code>assay</code> with <code>-library-url</code> pointing
              at your read-only R2/CDN so it doesn’t fall back to the public
              path.
            </p>
          ) : null}
          <div className="flex flex-wrap gap-2">
            <Button size="sm" variant="outline" onClick={onRetry}>
              <RefreshCw className="size-4" />
              Retry
            </Button>
          </div>
          <details className="text-xs">
            <summary className="cursor-pointer font-medium">
              Technical details
            </summary>
            <p className="mt-2 font-mono break-words text-muted-foreground">
              {msg}
            </p>
          </details>
        </AlertDescription>
      </Alert>
    )
  }

  const done = Number(progress?.bytesDone ?? 0n)
  const total = Number(progress?.bytesTotal ?? 0n)
  const bytePct = total > 0 ? (done / total) * 100 : undefined
  const hasMeasuredProgress =
    (progress?.percent ?? 0) > 0 || done > 0 || total > 0
  const lastUpdateAge = lastUpdateAt
    ? Math.max(0, (Date.now() - lastUpdateAt) / 1000)
    : null
  const status =
    phase === 'done'
      ? 'Preparation complete'
      : phase === 'cancelled'
        ? 'Preparation cancelled'
        : progress?.stage || 'Contacting server…'

  return (
    <div className="space-y-3 rounded-md border border-border bg-muted/30 p-4">
      <div className="flex items-center justify-between text-sm">
        <span className="flex items-center gap-2 font-medium">
          {phase === 'done' ? (
            <CheckCircle2 className="size-4 text-success" />
          ) : null}
          {status}
        </span>
        {phase === 'cancelled' ? (
          <Badge variant="warning">cancelled</Badge>
        ) : (
          <span className="tabular-nums text-muted-foreground">
            {phase === 'done'
              ? 'confirmed'
              : hasMeasuredProgress
                ? `${progress?.percent ?? 0}% complete`
                : 'working'}
          </span>
        )}
      </div>

      <Progress
        value={phase === 'done' ? 100 : (progress?.percent ?? 0)}
        indeterminate={phase === 'streaming' && !hasMeasuredProgress}
      />

      {phase === 'done' ? (
        <p className="text-xs text-muted-foreground">
          The server confirmed the requested data is ready. This may be a
          download, a venue fetch, or a local-cache check.
        </p>
      ) : null}

      {progress?.currentFile ? (
        <p className="truncate font-mono text-xs text-muted-foreground">
          {progress.currentFile}
        </p>
      ) : null}

      {total > 0 ? (
        <div className="space-y-1">
          <Progress value={bytePct ?? 0} className="h-1.5" />
          <p className="text-xs tabular-nums text-muted-foreground">
            {formatBytes(progress?.bytesDone)} processed in the current file
          </p>
        </div>
      ) : null}

      {phase === 'streaming' || phase === 'done' ? (
        <div className="space-y-1 text-xs tabular-nums text-muted-foreground">
          <p>
            {updates} server update{updates === 1 ? '' : 's'} ·{' '}
            {elapsed.toFixed(0)}s elapsed
          </p>
          {phase === 'streaming' && lastUpdateAge !== null ? (
            <p>
              Last update{' '}
              {lastUpdateAge < 1
                ? 'just now'
                : `${lastUpdateAge.toFixed(0)}s ago`}
            </p>
          ) : null}
        </div>
      ) : null}

      {phase === 'streaming' && updates === 0 && elapsed > 4 ? (
        <p className="text-xs text-warning">
          Connected, but no status events have arrived in {elapsed.toFixed(0)}s.
          The server may still be reaching the data source. If this never
          advances, check the server’s connectivity or its{' '}
          <code>-library-url</code>.
        </p>
      ) : null}

      {phase === 'streaming' &&
      updates > 0 &&
      !hasMeasuredProgress &&
      elapsed > 6 ? (
        <p className="text-xs text-muted-foreground">
          The server is responding but has not measured a completed data
          partition yet. It may still be checking the cache or contacting a
          source.
        </p>
      ) : null}
    </div>
  )
}

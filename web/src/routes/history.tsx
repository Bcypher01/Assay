import { createFileRoute } from '@tanstack/react-router'
import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  RefreshCw,
  XCircle,
  ArrowRight,
  GitCompareArrows,
  AlertTriangle,
} from 'lucide-react'
import { PageHeader } from '@/components/page-header'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { runApi, rpcErrorMessage } from '@/lib/rpc'
import { useSession } from '@/lib/store'
import { formatUsdCents, shortHash } from '@/lib/codec'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/history')({ component: HistoryScreen })

interface Summary {
  trades: number
  unresolved: number
  error: string
  runId: string
}

interface ParamChange {
  path: string
  from: string
  to: string
}

interface MetricDelta {
  name: string
  pessimistic: number
  optimistic: number
}

interface CompareResult {
  error: string
  changes: ParamChange[]
  metricDeltas: MetricDelta[]
}

function HistoryScreen() {
  const s = useSession()
  const [hashes, setHashes] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [listError, setListError] = useState<string | null>(null)

  const [detailHash, setDetailHash] = useState<string | null>(null)
  const [detail, setDetail] = useState<Summary | null>(null)
  const [detailError, setDetailError] = useState<string | null>(null)

  const [left, setLeft] = useState<string>('')
  const [right, setRight] = useState<string>('')
  const [comparing, setComparing] = useState(false)
  const [compare, setCompare] = useState<CompareResult | null>(null)
  const [compareError, setCompareError] = useState<string | null>(null)

  const loadRuns = useCallback(async () => {
    setLoading(true)
    setListError(null)
    try {
      const res = await runApi.listRuns({})
      setHashes(res.hashes)
    } catch (err) {
      setListError(rpcErrorMessage(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadRuns()
  }, [loadRuns])

  // Prefill compare/detail with the most recent run from this session.
  useEffect(() => {
    if (s.lastRunId && !left) setLeft(s.lastRunId)
  }, [s.lastRunId, left])

  const openDetail = async (hash: string) => {
    setDetailHash(hash)
    setDetail(null)
    setDetailError(null)
    try {
      const res = await runApi.getRun({ hash })
      setDetail({
        trades: res.trades,
        unresolved: res.unresolved,
        error: res.error,
        runId: res.runId,
      })
    } catch (err) {
      setDetailError(rpcErrorMessage(err))
    }
  }

  const runCompare = async () => {
    if (!left || !right) return
    setComparing(true)
    setCompare(null)
    setCompareError(null)
    try {
      const res = await runApi.compare({ left, right })
      setCompare({
        error: res.error,
        changes: res.changes.map((c) => ({
          path: c.path,
          from: c.from,
          to: c.to,
        })),
        metricDeltas: res.metricDeltas.map((m) => ({
          name: m.name,
          pessimistic: m.pessimistic,
          optimistic: m.optimistic,
        })),
      })
    } catch (err) {
      setCompareError(rpcErrorMessage(err))
    } finally {
      setComparing(false)
    }
  }

  const options = useMemo(() => hashes, [hashes])

  return (
    <div className="space-y-8">
      <PageHeader
        step={6}
        title="History & Compare"
        description="Every run is addressed by its content hash. Inspect a run’s summary, or compare two to see ordered parameter changes and bounded metric deltas."
        actions={
          <Button
            variant="outline"
            size="sm"
            onClick={() => void loadRuns()}
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
          <AlertTitle>Couldn’t load run history</AlertTitle>
          <AlertDescription>{listError}</AlertDescription>
        </Alert>
      ) : null}

      <div className="grid gap-6 lg:grid-cols-[minmax(0,320px)_1fr]">
        <Card className="lg:max-h-[70vh] lg:overflow-hidden">
          <CardHeader className="pb-3">
            <CardTitle>Runs</CardTitle>
            <CardDescription>
              {loading ? 'Loading…' : `${hashes.length} runs`}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-1.5 lg:max-h-[56vh] lg:overflow-y-auto">
            {loading && hashes.length === 0
              ? Array.from({ length: 7 }).map((_, i) => (
                  <Skeleton key={i} className="h-[38px] w-full" />
                ))
              : null}
            {!loading && hashes.length === 0 && !listError ? (
              <p className="text-sm text-muted-foreground">
                No runs yet. Complete a run to see it here.
              </p>
            ) : null}
            {hashes.map((h) => (
              <button
                key={h}
                type="button"
                onClick={() => void openDetail(h)}
                className={cn(
                  'flex w-full items-center justify-between gap-2 rounded-md border px-3 py-2 text-left transition-colors',
                  detailHash === h
                    ? 'border-primary bg-primary/5'
                    : 'border-border hover:bg-accent',
                )}
              >
                <code className="truncate font-mono text-xs" title={h}>
                  {shortHash(h, 16)}
                </code>
                {s.lastRunId === h ? (
                  <Badge variant="secondary">latest</Badge>
                ) : null}
              </button>
            ))}
          </CardContent>
        </Card>

        <div className="space-y-6">
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base">Run detail</CardTitle>
              <CardDescription>
                {detailHash ? (
                  <code className="font-mono">{shortHash(detailHash, 20)}</code>
                ) : (
                  'Select a run to inspect its summary.'
                )}
              </CardDescription>
            </CardHeader>
            <CardContent>
              {detailError ? (
                <Alert variant="destructive">
                  <XCircle />
                  <AlertDescription>{detailError}</AlertDescription>
                </Alert>
              ) : detail ? (
                <div className="space-y-3">
                  {detail.error ? (
                    <Alert variant="destructive">
                      <XCircle />
                      <AlertDescription>{detail.error}</AlertDescription>
                    </Alert>
                  ) : null}
                  <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                    <Stat
                      label="Trades"
                      value={detail.trades.toLocaleString()}
                    />
                    <Stat
                      label="Unresolved"
                      value={detail.unresolved.toLocaleString()}
                      tone={detail.unresolved > 0 ? 'warning' : undefined}
                    />
                  </div>
                  <div className="flex flex-wrap gap-2 pt-1">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setLeft(detail.runId || detailHash || '')}
                    >
                      Use as left
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setRight(detail.runId || detailHash || '')}
                    >
                      Use as right
                    </Button>
                  </div>
                </div>
              ) : detailHash ? (
                <div className="space-y-3">
                  <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                    {Array.from({ length: 2 }).map((_, i) => (
                      <div key={i} className="space-y-2">
                        <Skeleton className="h-3 w-16" />
                        <Skeleton className="h-6 w-12" />
                      </div>
                    ))}
                  </div>
                  <div className="flex gap-2 pt-1">
                    <Skeleton className="h-8 w-24" />
                    <Skeleton className="h-8 w-24" />
                  </div>
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">
                  Nothing selected.
                </p>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="flex items-center gap-2 text-base">
                <GitCompareArrows className="size-4" />
                Compare two runs
              </CardTitle>
              <CardDescription>
                Parameter changes are ordered; metric deltas are bounded
                [pessimistic, optimistic].
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid gap-3 sm:grid-cols-[1fr_auto_1fr]">
                <HashSelect
                  label="Left"
                  value={left}
                  onChange={setLeft}
                  options={options}
                />
                <div className="hidden items-end pb-2 sm:flex">
                  <ArrowRight className="size-4 text-muted-foreground" />
                </div>
                <HashSelect
                  label="Right"
                  value={right}
                  onChange={setRight}
                  options={options}
                />
              </div>
              <Button
                onClick={() => void runCompare()}
                disabled={!left || !right || left === right || comparing}
              >
                <GitCompareArrows className="size-4" />
                {comparing ? 'Comparing…' : 'Compare'}
              </Button>
              {left && right && left === right ? (
                <p className="text-xs text-warning">
                  Pick two different runs to compare.
                </p>
              ) : null}

              {compareError ? (
                <Alert variant="destructive">
                  <XCircle />
                  <AlertDescription>{compareError}</AlertDescription>
                </Alert>
              ) : null}

              {compare ? <CompareView result={compare} /> : null}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}

function CompareView({ result }: { result: CompareResult }) {
  return (
    <div className="space-y-5 pt-2">
      {result.error ? (
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertDescription>{result.error}</AlertDescription>
        </Alert>
      ) : null}

      <div className="space-y-2">
        <h3 className="text-sm font-semibold">
          Parameter changes ({result.changes.length})
        </h3>
        {result.changes.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No parameter differences.
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Path</TableHead>
                <TableHead>From</TableHead>
                <TableHead>To</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {result.changes.map((c, i) => (
                <TableRow key={`${c.path}-${i}`}>
                  <TableCell className="font-mono text-xs">{c.path}</TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {c.from}
                  </TableCell>
                  <TableCell className="font-mono text-xs">{c.to}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      <div className="space-y-2">
        <h3 className="text-sm font-semibold">
          Metric deltas ({result.metricDeltas.length})
        </h3>
        {result.metricDeltas.length === 0 ? (
          <p className="text-sm text-muted-foreground">No metric deltas.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Metric</TableHead>
                <TableHead className="text-right">Pessimistic</TableHead>
                <TableHead className="text-right">Optimistic</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {result.metricDeltas.map((m, i) => (
                <TableRow key={`${m.name}-${i}`}>
                  <TableCell className="text-sm">{m.name}</TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {formatMetricDelta(m.name, m.pessimistic)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {formatMetricDelta(m.name, m.optimistic)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>
    </div>
  )
}

function formatMetricDelta(name: string, n: number): string {
  if (!Number.isFinite(n)) return '—'
  if (/equity|pnl|drawdown|expectancy|cost|profit(?!_?factor)/i.test(name)) {
    return formatUsdCents(n, { sign: true })
  }
  const sign = n > 0 ? '+' : ''
  return `${sign}${n.toLocaleString(undefined, { maximumFractionDigits: 4 })}`
}

function HashSelect({
  label,
  value,
  onChange,
  options,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  options: string[]
}) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger>
          <SelectValue placeholder="Select run" />
        </SelectTrigger>
        <SelectContent>
          {options.map((h) => (
            <SelectItem key={h} value={h}>
              {shortHash(h, 18)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

function Stat({
  label,
  value,
  tone,
}: {
  label: string
  value: string
  tone?: 'warning'
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
    </div>
  )
}

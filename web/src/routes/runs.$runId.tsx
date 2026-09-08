import { createFileRoute, Link } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import {
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  FlaskConical,
  GitCompareArrows,
} from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatUsdCents, prettyJson } from '@/lib/codec'
import { rpcErrorMessage, runApi } from '@/lib/rpc'
import { session } from '@/lib/store'

export const Route = createFileRoute('/runs/$runId')({ component: ResultPage })
type Details = NonNullable<
  Awaited<ReturnType<typeof runApi.getDetails>>['details']
>
type Trades = Awaited<ReturnType<typeof runApi.getTrades>>
type Bounded = { pessimistic: number; optimistic: number } | undefined

const bounded = (v: Bounded, digits = 2) =>
  !v
    ? '—'
    : v.pessimistic === v.optimistic
      ? v.pessimistic.toLocaleString(undefined, {
          maximumFractionDigits: digits,
        })
      : `${v.pessimistic.toLocaleString(undefined, { maximumFractionDigits: digits })} – ${v.optimistic.toLocaleString(undefined, { maximumFractionDigits: digits })}`
const moneyBounded = (v: Bounded) =>
  !v
    ? '—'
    : v.pessimistic === v.optimistic
      ? formatUsdCents(v.pessimistic)
      : `${formatUsdCents(v.pessimistic)} – ${formatUsdCents(v.optimistic)}`
const moneyResult = (v: Bounded) => {
  if (!v) return '—'
  if (v.pessimistic !== v.optimistic)
    return `${formatUsdCents(v.pessimistic, { sign: true })} – ${formatUsdCents(v.optimistic, { sign: true })}`
  return formatUsdCents(v.pessimistic, { sign: true })
}
const date = (v: bigint) => (v ? new Date(Number(v)).toLocaleDateString() : '—')
const utcDate = new Intl.DateTimeFormat(undefined, {
  timeZone: 'UTC',
  day: '2-digit',
  month: 'short',
  year: 'numeric',
})
/**
 * The server names a period by its epoch-ms bounds. Two thirteen-digit numbers
 * are an identifier, not a date range a person can read, so render them as one
 * where the shape allows it and fall back to the raw label otherwise.
 */
const period = (value: string) => {
  const match = /^(\d{10,})-(\d{10,})$/.exec(value)
  if (!match) return value
  return `${utcDate.format(Number(match[1]))} – ${utcDate.format(Number(match[2]))}`
}
/**
 * Content-addressed hashes are 64 characters and belong to machines. Showing
 * the whole thing is what pushes these cards past their own edges, so the head
 * and tail are shown (enough to tell two runs apart) with the full value on
 * hover and available to copy.
 */
const shortHash = (value: string) =>
  value.length > 20 ? `${value.slice(0, 10)}…${value.slice(-6)}` : value

function Hash({ value }: { value: string }) {
  if (!value) return <span className="text-muted-foreground">—</span>
  return (
    <span
      className="cursor-help font-mono text-xs break-all"
      title={value}
      onClick={() => void navigator.clipboard?.writeText(value)}
    >
      {shortHash(value)}
    </span>
  )
}
const empty = (
  <p className="text-sm text-muted-foreground">
    This check was not kept for this run.
  </p>
)

function ResultPage() {
  const { runId } = Route.useParams()
  const [details, setDetails] = useState<Details>()
  const [trades, setTrades] = useState<Trades>()
  const [offset, setOffset] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [tradesError, setTradesError] = useState<string | null>(null)
  const [runIds, setRunIds] = useState<string[]>([])
  const [compareId, setCompareId] = useState('')
  const [comparison, setComparison] =
    useState<Awaited<ReturnType<typeof runApi.compare>>>()
  const [comparisonError, setComparisonError] = useState<string | null>(null)
  const [comparing, setComparing] = useState(false)
  useEffect(() => {
    setDetails(undefined)
    setTrades(undefined)
    setError(null)
    setTradesError(null)
    setOffset(0)
    void runApi
      .getDetails({ hash: runId })
      .then(({ details: resultDetails }) =>
        resultDetails
          ? setDetails(resultDetails)
          : setError('The server returned no details for this run.'),
      )
      .catch((e) => setError(rpcErrorMessage(e)))
  }, [runId])
  useEffect(() => {
    if (!details?.tradeListPresent) return
    setTradesError(null)
    void runApi
      .getTrades({ hash: runId, pageSize: 100, pageOffset: offset })
      .then(setTrades)
      .catch((e) => setTradesError(rpcErrorMessage(e)))
  }, [details?.tradeListPresent, offset, runId])
  useEffect(() => {
    void runApi
      .listRuns({})
      .then((response) => setRunIds(response.hashes))
      .catch(() => setRunIds([]))
  }, [])
  if (error)
    return (
      <Alert variant="destructive">
        <AlertTitle>Couldn’t load this result</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    )
  if (!details) return <ResultSkeleton />
  const out = details.outOfSample
  const verdict = evidenceVerdict(details)
  const wins = out?.wins ?? 0
  const losses = out?.losses ?? 0
  const decidedTrades = wins + losses
  const winRate = decidedTrades > 0 ? (wins / decidedTrades) * 100 : null
  const startExperiment = (kind: string) => {
    try {
      const study = JSON.parse(session.get().strategyText) as Record<
        string,
        unknown
      >
      session.set({
        strategyText: prettyJson({
          ...study,
          name: `${String(study.name || 'Study')} — ${kind || 'experiment'}`,
        }),
        validatedHash: null,
      })
    } catch {
      setError(
        'Your current strategy cannot be copied because it is not valid JSON.',
      )
    }
  }
  const compare = async () => {
    if (!compareId) return
    setComparing(true)
    setComparison(undefined)
    setComparisonError(null)
    try {
      setComparison(
        await runApi.compare({
          left: details.runId || runId,
          right: compareId,
        }),
      )
    } catch (cause) {
      setComparisonError(rpcErrorMessage(cause))
    } finally {
      setComparing(false)
    }
  }
  return (
    <div className="page-form">
      <header>
        <p className="kicker">Tested on {period(details.period)}</p>
        <h1 className="text-3xl font-semibold">
          {details.name || 'Strategy result'}
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {details.instruments.join(', ')} · This is what the rules did on past
          prices. It is not a prediction and not advice.
        </p>
      </header>

      {details.unresolved ? (
        <Alert variant="warning">
          <AlertTriangle />
          <AlertTitle>
            {details.tradeListPresent
              ? 'Ambiguous trades are excluded from these results'
              : 'Some exits could have gone either way'}
          </AlertTitle>
          <AlertDescription>
            On {details.unresolved} occasion
            {details.unresolved === 1 ? '' : 's'} the stop and the target were
            both reachable inside the same bar, and the data cannot say which
            came first.{' '}
            {details.tradeListPresent
              ? 'Those trades are listed under “Every detail,” but do not contribute to the headline profit, balance, win rate, or drawdown.'
              : 'Their original range is retained because the individual trades are no longer stored for this older run.'}
          </AlertDescription>
        </Alert>
      ) : null}

      {details.unfilled ? (
        <Alert variant="warning">
          <AlertTriangle />
          <AlertTitle>Some entries never filled</AlertTitle>
          <AlertDescription>
            {details.unfilled} entry signal
            {details.unfilled === 1 ? '' : 's'} placed a limit or stop order
            that the following bar never reached, so no position was opened. The
            order is good for that bar only; it does not rest until price comes
            back. Fewer trades here is the strategy&rsquo;s own behaviour, not
            missing setups.
          </AlertDescription>
        </Alert>
      ) : null}

      {/* The verdict forms the headline answer — a single spoken judgement,
          then the failing checks as a short list. Nothing here is buried. */}
      <section className="verdict-hero" data-tone={verdict.tone}>
        <div className="verdict-head">
          <p className="verdict-word">{verdict.label}</p>
        </div>
        <p className="verdict-dek">{verdict.body}</p>
        {verdict.reasons.length > 0 ? (
          <ul className="verdict-checks">
            {verdict.reasons.map((reason) => (
              <li key={reason.check}>
                <span className="verdict-check-name">{reason.check}</span>
                <span className="verdict-check-detail">{reason.detail}</span>
              </li>
            ))}
          </ul>
        ) : null}
      </section>

      <section className="ledger">
        <Metric
          label="Ending balance"
          hint="what the account was worth at the end"
          value={moneyBounded(out?.finalEquity)}
          tone={equityTone(out?.finalEquity?.pessimistic, details)}
        />
        <Metric
          label="Win rate"
          hint="wins divided by decided trades"
          value={winRate === null ? '—' : `${winRate.toFixed(1)}%`}
        />
        <Metric
          label="Wins / losses"
          hint="break-even trades are excluded"
          value={`${wins.toLocaleString()} / ${losses.toLocaleString()}`}
        />
        <Metric
          label="Trades"
          hint="positions taken in the unseen period"
          value={(out?.trades ?? 0).toLocaleString()}
        />
        <Metric
          label="Worst fall"
          hint="largest drop from a high point before recovering"
          value={moneyBounded(out?.maxDrawdown)}
          tone={toneFor(out?.maxDrawdown?.pessimistic)}
        />
        <Metric
          label="Profit factor"
          hint="money won for every 1 lost — under 1 loses money"
          value={bounded(out?.profitFactor)}
        />
        <Metric
          label="Average per trade"
          hint="what a single trade was worth on average"
          value={moneyBounded(out?.expectancy)}
          tone={toneFor(out?.expectancy?.pessimistic)}
        />
      </section>

      <NextActions actions={details.nextActions} onStart={startExperiment} />

      <Tabs defaultValue="summary">
        <TabsList>
          <TabsTrigger value="summary">Summary</TabsTrigger>
          <TabsTrigger value="robustness">Does it hold up?</TabsTrigger>
          <TabsTrigger value="detail">Every detail</TabsTrigger>
        </TabsList>

        <TabsContent value="summary" className="space-y-6 pt-4">
          <SampleTable details={details} />
          <section className="grid gap-6 lg:grid-cols-2">
            <Card className="min-w-0">
              <CardHeader>
                <CardTitle>What this is based on</CardTitle>
              </CardHeader>
              <CardContent className="min-w-0 space-y-2 text-sm">
                <p>
                  {out?.trades ?? 0} trade
                  {(out?.trades ?? 0) === 1 ? '' : 's'} on the unseen stretch of
                  history. This strategy has been run {details.trials} time
                  {details.trials === 1 ? '' : 's'} — every attempt is counted,
                  so a rule that took twenty tries cannot look like it took one.
                </p>
                <p>
                  {details.tradeListPresent
                    ? 'Each individual trade is kept, and listed under “Every detail”.'
                    : 'The individual trades were dropped by your local history limit, so they cannot be listed.'}
                </p>
                <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 pt-1 text-xs text-muted-foreground">
                  <dt>Strategy</dt>
                  <dd className="min-w-0">
                    <Hash value={details.strategyHash} />
                  </dd>
                  <dt>Data snapshot</dt>
                  <dd className="min-w-0">
                    <Hash value={details.snapshot} />
                  </dd>
                </dl>
              </CardContent>
            </Card>
            <Assumptions details={details} />
          </section>
        </TabsContent>

        <TabsContent value="robustness" className="space-y-6 pt-4">
          <p className="text-sm text-muted-foreground">
            A rule can look good on one stretch of history by luck. These four
            checks try to break it. A rule that survives all four is not proven
            — it has just failed to be disproven in the obvious ways.
          </p>
          <WalkForward rows={details.walkForward} />
          <CostStress rows={details.costStress} />
          <Sensitivity grid={details.sensitivity} />
          <MonteCarlo summary={details.monteCarlo} />
        </TabsContent>

        <TabsContent value="detail" className="space-y-6 pt-4">
          <PeriodTable title="Year by year" rows={details.yearly} />
          <PeriodTable title="Month by month" rows={details.monthly} />
          <Card>
            <CardHeader>
              <CardTitle>Conditions that almost fired</CardTitle>
              <CardDescription>
                Moments that met most of the entry rules but not all of them. A
                long list here means the rules are close to triggering often, so
                small changes could move the result a lot.
              </CardDescription>
            </CardHeader>
            <CardContent>
              {details.nearMisses.length ? (
                <ul className="space-y-2 text-sm">
                  {details.nearMisses.map((x) => (
                    <li
                      key={x.condition}
                      className="flex justify-between gap-4 border-b pb-2"
                    >
                      <span className="min-w-0 break-words">{x.condition}</span>
                      <span className="shrink-0 font-mono">
                        {x.count.toLocaleString()}
                      </span>
                    </li>
                  ))}
                </ul>
              ) : (
                empty
              )}
            </CardContent>
          </Card>
          <TradeInspector
            available={details.tradeListPresent}
            trades={trades}
            error={tradesError}
            offset={offset}
            previous={() => setOffset(Math.max(0, offset - 100))}
            next={() => setOffset(trades?.nextOffset ?? offset)}
          />
          <RunComparison
            runId={details.runId || runId}
            options={runIds}
            value={compareId}
            result={comparison}
            error={comparisonError}
            loading={comparing}
            onChange={setCompareId}
            onCompare={() => void compare()}
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function Assumptions({ details }: { details: Details }) {
  const a = details.assumptions
  return (
    <Card>
      <CardHeader>
        <CardTitle>What was assumed about execution</CardTitle>
        <CardDescription>
          The costs and fill rules the simulation charged you.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid grid-cols-2 gap-3 text-sm">
        <Pair
          label="Starting balance"
          value={a ? formatUsdCents(a.initialEquity) : '—'}
        />
        <Pair
          label="Risk per trade"
          value={`${((a?.riskPerTrade ?? 0) * 100).toFixed(2)}%`}
        />
        <Pair
          label="Spread override"
          value={a?.spreadTicks.toString() ?? '—'}
        />
        <Pair
          label="Slippage override"
          value={a?.slippageTicks.toString() ?? '—'}
        />
        <Pair label="Commission" value={`${a?.commissionBps ?? 0} bps`} />
        <Pair
          label="Flat commission"
          value={a ? formatUsdCents(a.commissionFlat) : '—'}
        />
        <Pair label="Fills happen" value={a?.fillPolicy || 'Next bar open'} />
      </CardContent>
    </Card>
  )
}
function SampleTable({ details }: { details: Details }) {
  const rows: Array<
    [string, Bounded | number | undefined, Bounded | number | undefined]
  > = [
    [
      'Ending balance',
      details.inSample?.finalEquity,
      details.outOfSample?.finalEquity,
    ],
    [
      'Worst fall',
      details.inSample?.maxDrawdown,
      details.outOfSample?.maxDrawdown,
    ],
    [
      'Profit factor',
      details.inSample?.profitFactor,
      details.outOfSample?.profitFactor,
    ],
    [
      'Average per trade',
      details.inSample?.expectancy,
      details.outOfSample?.expectancy,
    ],
    ['Number of trades', details.inSample?.trades, details.outOfSample?.trades],
  ]
  const show = (label: string, v: Bounded | number | undefined) => {
    if (typeof v === 'number') return v.toLocaleString()
    return ['Ending balance', 'Worst fall', 'Average per trade'].includes(label)
      ? moneyBounded(v)
      : bounded(v)
  }
  return (
    <Evidence
      title="Fitted period vs unseen period"
      description="The left column is the stretch the rules were built against, so it flatters them by construction. The right column is the stretch they had never seen, and is the one that counts. Where a value is a range, the worse case comes first."
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Metric</TableHead>
            <TableHead>Fitted period</TableHead>
            <TableHead>Unseen period</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map(([n, i, o]) => (
            <TableRow key={n}>
              <TableCell>{n}</TableCell>
              <TableCell>{show(n, i)}</TableCell>
              <TableCell>{show(n, o)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Evidence>
  )
}
function WalkForward({ rows }: { rows: Details['walkForward'] }) {
  return (
    <Evidence
      title="Tested on history it had not seen"
      description="The rules were fitted on an early stretch, then run on the stretch that followed, over and over. If the average per trade stays positive on the stretches it never saw, the result is less likely to be a quirk of one period."
    >
      {rows.length ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Fitted on</TableHead>
              <TableHead>Then tested on</TableHead>
              <TableHead>Average per trade</TableHead>
              <TableHead>Trades</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r, i) => (
              <TableRow key={`${r.trainFrom}-${i}`}>
                <TableCell>
                  {r.trainFrom} – {r.trainTo}
                </TableCell>
                <TableCell>
                  {r.testFrom} – {r.testTo}
                </TableCell>
                <TableCell>{moneyBounded(r.outOfSample?.expectancy)}</TableCell>
                <TableCell>
                  {r.outOfSample?.trades.toLocaleString() ?? '—'}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        empty
      )}
    </Evidence>
  )
}
function CostStress({ rows }: { rows: Details['costStress'] }) {
  return (
    <Evidence
      title="If trading cost more than you assumed"
      description="The same trades re-priced with your spread, slippage and commission doubled and tripled. A rule that only works at 1× is a rule that depends on getting perfect fills forever."
    >
      {rows.length ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Costs</TableHead>
              <TableHead>Ending balance</TableHead>
              <TableHead>Worst fall</TableHead>
              <TableHead>Average per trade</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <TableRow key={r.multiplier}>
                <TableCell>{r.multiplier}×</TableCell>
                <TableCell>{moneyBounded(r.metrics?.finalEquity)}</TableCell>
                <TableCell>{moneyBounded(r.metrics?.maxDrawdown)}</TableCell>
                <TableCell>{moneyBounded(r.metrics?.expectancy)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        empty
      )}
    </Evidence>
  )
}
function Sensitivity({ grid }: { grid: Details['sensitivity'] }) {
  return (
    <Evidence
      title="The settings either side of yours"
      description="The same rule at neighbouring risk and cost settings — each cell is the average per trade. If the result collapses one cell away from yours, you found a lucky setting rather than an edge. Green survived the pessimistic case, amber only the optimistic one, red neither."
    >
      {grid?.x.length && grid.y.length ? (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Risk \ Costs</TableHead>
                {grid.x.map((x) => (
                  <TableHead key={x}>{x}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {grid.y.map((y, yi) => (
                <TableRow key={y}>
                  <TableCell className="font-medium">{y}</TableCell>
                  {grid.x.map((_, xi) => {
                    const expectancy =
                      grid.cells[yi * grid.x.length + xi]?.metrics?.expectancy
                    return (
                      <TableCell key={xi} className={heatTone(expectancy)}>
                        {moneyBounded(expectancy)}
                      </TableCell>
                    )
                  })}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        empty
      )}
    </Evidence>
  )
}
function heatTone(value: Bounded) {
  if (!value) return 'bg-muted/30'
  if (value.pessimistic > 0) return 'bg-success/15'
  if (value.optimistic > 0) return 'bg-warning/15'
  return 'bg-destructive/10'
}
function MonteCarlo({ summary }: { summary: Details['monteCarlo'] }) {
  return (
    <Evidence
      title="If the same trades had come in a different order"
      description="A backtest runs the trades in the one order history happened to deal them. Here that order was shuffled a thousand times over to see how much of the result rode on it. When this run beats the typical shuffle by a lot, the ordering flattered it."
    >
      {summary ? (
        <>
          <div className="space-y-4">
            <ScenarioReading
              noun="ending balance"
              band={summary.finalEquity}
              actual={summary.realisedFinalEquity}
              higherIsBetter
            />
            <ScenarioReading
              noun="worst fall"
              band={summary.maxDrawdown}
              actual={summary.realisedMaxDrawdown}
              higherIsBetter={false}
            />
          </div>
          <p className="mt-5 border-t border-border/60 pt-3 text-xs text-muted-foreground">
            {summary.samples.toLocaleString()} reshuffles · chunk length{' '}
            {summary.blockLength} · seed {summary.seed.toString()}, which
            reproduces these figures exactly
          </p>
        </>
      ) : (
        empty
      )}
    </Evidence>
  )
}

/**
 * One metric stated in plain words: what this run got, how that compares with
 * the typical reshuffle, and the lucky-to-unlucky range behind it. No chart —
 * the sentence is the point. The comparison is oriented by outcome (for a
 * drawdown the smaller fall is the better one), so "better" always means the
 * result came out ahead.
 */
function ScenarioReading({
  noun,
  band,
  actual,
  higherIsBetter,
}: {
  noun: string
  band: { p05: number; p50: number; p95: number } | undefined
  actual: number
  higherIsBetter: boolean
}) {
  const fmt = (n: number) => formatUsdCents(n)
  if (!band) {
    return (
      <p className="border-t border-border/60 pt-4 text-sm first:border-t-0 first:pt-0">
        <span className="font-medium capitalize">{noun}</span>{' '}
        <span className="text-muted-foreground">was not recorded.</span>
      </p>
    )
  }
  const typical = band.p50
  const lucky = higherIsBetter ? band.p95 : band.p05
  const unlucky = higherIsBetter ? band.p05 : band.p95
  // How far this run beat the typical shuffle, measured against the typical-to-
  // lucky gap so the wording scales with how wide the range actually is.
  const ahead = higherIsBetter ? actual - typical : typical - actual
  const reach = Math.abs(lucky - typical) || 1
  const share = ahead / reach
  const comparison =
    share > 0.66
      ? { text: 'much better than', tone: 'text-warning' }
      : share > 0.15
        ? { text: 'better than', tone: 'text-warning' }
        : share < -0.66
          ? { text: 'much worse than', tone: 'text-foreground' }
          : share < -0.15
            ? { text: 'worse than', tone: 'text-foreground' }
            : { text: 'about the same as', tone: 'text-foreground' }
  return (
    <p className="border-t border-border/60 pt-4 text-sm leading-relaxed first:border-t-0 first:pt-0">
      This run’s <span className="font-medium">{noun}</span> was{' '}
      <span className="font-mono font-semibold">{fmt(actual)}</span>,{' '}
      <span className={`font-medium ${comparison.tone}`}>
        {comparison.text}
      </span>{' '}
      a typical reshuffle (<span className="font-mono">{fmt(typical)}</span>).{' '}
      <span className="text-muted-foreground">
        Across the reshuffles it ranged from{' '}
        <span className="font-mono">{fmt(unlucky)}</span> at its unluckiest to{' '}
        <span className="font-mono">{fmt(lucky)}</span> at its luckiest.
      </span>
    </p>
  )
}
function NextActions({
  actions,
  onStart,
}: {
  actions: Details['nextActions']
  onStart: (kind: string) => void
}) {
  return actions.length ? (
    <Evidence
      title="What to try next"
      description="Each of these starts a fresh version of the strategy. It is counted as another attempt, because trying twenty variations and keeping the best one is how a rule comes to look better than it is."
    >
      <div className="grid gap-3 md:grid-cols-2">
        {actions.map((a, i) => (
          <div key={`${a.kind}-${i}`} className="rounded-md border p-4">
            <div className="flex items-center gap-2">
              <FlaskConical className="size-4 text-primary" />
              <p className="font-medium">{a.kind || 'Experiment'}</p>
            </div>
            <p className="mt-2 text-sm text-muted-foreground">{a.reason}</p>
            <Button asChild className="mt-4" size="sm" variant="outline">
              <Link to="/strategy" onClick={() => onStart(a.kind)}>
                Set up experiment
              </Link>
            </Button>
          </div>
        ))}
      </div>
    </Evidence>
  ) : null
}
function RunComparison({
  runId,
  options,
  value,
  result,
  error,
  loading,
  onChange,
  onCompare,
}: {
  runId: string
  options: string[]
  value: string
  result: Awaited<ReturnType<typeof runApi.compare>> | undefined
  error: string | null
  loading: boolean
  onChange: (value: string) => void
  onCompare: () => void
}) {
  const choices = options.filter((id) => id !== runId)
  return (
    <Evidence
      title="Compare with another run"
      description="Put this result next to an earlier attempt to see exactly which settings changed and what each change did to the numbers."
    >
      <div className="flex flex-wrap gap-3">
        <div className="min-w-64 flex-1">
          <Select value={value} onValueChange={onChange}>
            <SelectTrigger>
              <SelectValue placeholder="Select another run" />
            </SelectTrigger>
            <SelectContent>
              {choices.map((id) => (
                <SelectItem key={id} value={id}>
                  <span className="font-mono text-xs" title={id}>
                    {shortHash(id)}
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button disabled={!value || loading} onClick={onCompare}>
          <GitCompareArrows />
          {loading ? 'Comparing…' : 'Compare runs'}
        </Button>
      </div>
      {error ? (
        <Alert className="mt-4" variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      {result ? (
        <div className="mt-5 grid gap-5 lg:grid-cols-2">
          {result.error ? (
            <Alert variant="destructive">
              <AlertDescription>{result.error}</AlertDescription>
            </Alert>
          ) : null}
          <div>
            <h3 className="mb-2 text-sm font-semibold">What changed</h3>
            {result.changes.length ? (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Path</TableHead>
                    <TableHead>From</TableHead>
                    <TableHead>To</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {result.changes.map((change, index) => (
                    <TableRow key={`${change.path}-${index}`}>
                      <TableCell className="font-mono text-xs">
                        {change.path}
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {change.from}
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {change.to}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            ) : (
              <p className="text-sm text-muted-foreground">
                Nothing changed between these two runs.
              </p>
            )}
          </div>
          <div>
            <h3 className="mb-2 text-sm font-semibold">
              What it did to the numbers
            </h3>
            {result.metricDeltas.length ? (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Metric</TableHead>
                    <TableHead>Pessimistic</TableHead>
                    <TableHead>Optimistic</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {result.metricDeltas.map((metric, index) => (
                    <TableRow key={`${metric.name}-${index}`}>
                      <TableCell>{metric.name}</TableCell>
                      <TableCell>
                        {formatMetricDelta(metric.name, metric.pessimistic)}
                      </TableCell>
                      <TableCell>
                        {formatMetricDelta(metric.name, metric.optimistic)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            ) : (
              <p className="text-sm text-muted-foreground">No numbers moved.</p>
            )}
          </div>
        </div>
      ) : null}
    </Evidence>
  )
}
type Reason = { check: string; detail: string }

function evidenceVerdict(details: Details) {
  const trades = details.outOfSample?.trades ?? 0
  const outcome = details.outOfSample?.expectancy?.pessimistic ?? 0

  // A strategy that never traded cannot fail any check — it produced no sample
  // to weigh. Every robustness panel (unseen, triple-cost, nearby-settings)
  // would read "worse than 1" purely because zero is not positive, which would
  // accuse a strategy of losing money it never had a chance to. Report it as
  // "no trades" with a single, honest reason instead of five invented ones.
  if (trades === 0) {
    return {
      label: 'No trades',
      title: 'The rules opened no positions on the tested history',
      body: 'Zero fills means there is nothing to judge a rule on — no profit, no loss, and no evidence either way. The strategy either produced no entry signals on this sample or never filled one. The “Every detail” tab shows the conditions that almost fired.',
      tone: 'warning' as const,
      reasons: [
        {
          check: 'Sample size',
          detail:
            '0 trades — a rule must actually trade before a backtest can say anything about it',
        },
      ],
    }
  }

  const unseenFails = details.walkForward.some(
    (window) => (window.outOfSample?.expectancy?.pessimistic ?? 0) <= 0,
  )
  const threeCost = details.costStress.find((point) => point.multiplier === 3)
  const costFails =
    !threeCost || (threeCost.metrics?.expectancy?.pessimistic ?? 0) <= 0
  const sensitivityFails =
    !details.sensitivity ||
    details.sensitivity.cells.length === 0 ||
    details.sensitivity.cells.some(
      (cell) => (cell.metrics?.expectancy?.pessimistic ?? 0) <= 0,
    )
  const outcomeFails = outcome <= 0
  const basePositive = !outcomeFails && trades >= 100
  if (basePositive && !unseenFails && !costFails && !sensitivityFails)
    return {
      label: 'Held up',
      title: 'Every check we ran still came out positive',
      body: 'It stayed profitable on history it had never seen, at triple the trading costs, and at the settings either side of yours. That is the strongest thing this tool can say, and it still only means the rule has not been disproven — not that it will work.',
      tone: 'default' as const,
      reasons: [] as Reason[],
    }
  if (
    !details.walkForward.length ||
    !details.costStress.length ||
    !details.sensitivity?.cells.length
  )
    return {
      label: 'Not enough to judge',
      title: 'Some of the checks are missing for this run',
      body: 'Answering “does this hold up?” needs the unseen-period, triple-cost and nearby-settings results, and at least one of them was not kept. The tab below shows which.',
      tone: 'warning' as const,
      reasons: [] as Reason[],
    }
  // Naming the specific check that failed is the difference between a verdict
  // a user can act on and one they can only feel bad about.
  const reasons = [
    trades < 100
      ? { check: 'Sample size', detail: `Only ${trades} trades to judge it on` }
      : null,
    outcomeFails
      ? { check: 'Tested period', detail: 'It lost money on the tested period' }
      : null,
    unseenFails
      ? {
          check: 'Unseen history',
          detail: 'It lost money on history it had not seen',
        }
      : null,
    costFails
      ? {
          check: 'Cost stress',
          detail: 'It stopped working at triple the trading costs',
        }
      : null,
    sensitivityFails
      ? {
          check: 'Nearby settings',
          detail: 'A nearby setting turns it into a loser',
        }
      : null,
  ].filter((reason): reason is Reason => reason !== null)
  return {
    label: 'Not there yet',
    title: `${reasons.length} ${reasons.length === 1 ? 'check' : 'checks'} flagged a problem`,
    body: 'The summary alone is not enough to rely on this result. “Does it hold up?” explains each check and shows the evidence.',
    tone: 'warning' as const,
    reasons,
  }
}
function formatMetricDelta(name: string, value: number) {
  if (/equity|pnl|drawdown|expectancy|cost|profit(?!_?factor)/i.test(name))
    return formatUsdCents(value, { sign: true })
  return `${value > 0 ? '+' : ''}${value.toLocaleString(undefined, { maximumFractionDigits: 4 })}`
}
function TradeInspector({
  available,
  trades,
  error,
  offset,
  previous,
  next,
}: {
  available: boolean
  trades: Trades | undefined
  error: string | null
  offset: number
  previous: () => void
  next: () => void
}) {
  return (
    <Evidence
      title="Every trade"
      description={
        available
          ? 'Each trade the rules took. Ambiguous exits are marked as excluded, so they cannot distort the reported result. Prices are shown in the source’s price points.'
          : 'Your local history limit dropped the individual trades for this run, so they cannot be listed.'
      }
    >
      {error ? (
        <Alert variant="warning">
          <AlertTitle>Trade list unavailable</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : available ? (
        <>
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Entry</TableHead>
                  <TableHead>Exit</TableHead>
                  <TableHead>Side</TableHead>
                  <TableHead>Entry / exit price points</TableHead>
                  <TableHead>Net result</TableHead>
                  <TableHead>Costs</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {trades?.trades.map((t, i) => (
                  <TableRow key={`${t.entryTimeMs}-${i}`}>
                    <TableCell>{date(t.entryTimeMs)}</TableCell>
                    <TableCell>
                      {t.ambiguous ? 'Unclear' : date(t.exitTimeMsPessimistic)}
                      {!t.ambiguous &&
                      t.exitTimeMsPessimistic !== t.exitTimeMsOptimistic
                        ? ` – ${date(t.exitTimeMsOptimistic)}`
                        : ''}
                    </TableCell>
                    <TableCell>
                      {t.side}
                      {t.ambiguous ? (
                        <Badge className="ml-2" variant="warning">
                          ambiguous
                        </Badge>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      {t.ambiguous
                        ? '—'
                        : `${t.entryPrice.toLocaleString()} / ${t.exitPricePessimistic.toLocaleString()}`}
                      {!t.ambiguous &&
                      t.exitPricePessimistic !== t.exitPriceOptimistic
                        ? ` – ${t.exitPriceOptimistic.toLocaleString()}`
                        : ''}
                    </TableCell>
                    <TableCell>
                      {t.ambiguous ? 'Excluded' : moneyResult(t.netPnl)}
                    </TableCell>
                    <TableCell>{moneyBounded(t.costs)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <div className="mt-4 flex items-center justify-between gap-3">
            <Button
              variant="outline"
              disabled={offset === 0}
              onClick={previous}
            >
              <ChevronLeft />
              Previous
            </Button>
            <span className="text-sm text-muted-foreground">
              {trades?.total ?? 0} trades
            </span>
            <Button
              variant="outline"
              disabled={!trades || trades.nextOffset === 0}
              onClick={next}
            >
              Next
              <ChevronRight />
            </Button>
          </div>
        </>
      ) : null}
    </Evidence>
  )
}
function Evidence({
  title,
  description,
  children,
}: {
  title: string
  description: string
  children: ReactNode
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  )
}
function Metric({
  label,
  value,
  hint,
  tone,
}: {
  label: string
  value: string
  hint?: string
  tone?: 'up' | 'down' | 'seal' | 'caution'
}) {
  return (
    <div className="ledger-cell" data-tone={tone}>
      <small>{label}</small>
      <b>{value}</b>
      {hint ? <em>{hint}</em> : null}
    </div>
  )
}
const toneFor = (value: number | undefined) => {
  if (value === undefined || value === 0) return undefined
  if (value < 0) return 'down' as const
  return 'up' as const
}
// Ending balance is only green or red by the change from the starting account,
// not by the absolute number — $100 ending is neutral when it began at $100.
const equityTone = (
  final: number | undefined,
  details: Details,
): 'up' | 'down' | undefined => {
  if (final === undefined) return undefined
  const start = Number(details.assumptions?.initialEquity ?? 0n)
  return toneFor(final - start)
}
function Pair({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-xs text-muted-foreground">{label}</p>
      <p>{value}</p>
    </div>
  )
}
function PeriodTable({
  title,
  rows,
}: {
  title: string
  rows: Details['yearly']
}) {
  return (
    <Evidence
      title={title}
      description="Net result after costs, excluding ambiguous exits. One dominant period is worth noticing — it means the whole result rests on a single stretch of history."
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Period</TableHead>
            <TableHead>Net result</TableHead>
            <TableHead>Trades</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((r) => (
            <TableRow key={r.period}>
              <TableCell>{r.period}</TableCell>
              <TableCell>{moneyResult(r.netPnl)}</TableCell>
              <TableCell>{r.trades.toLocaleString()}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Evidence>
  )
}

function ResultSkeleton() {
  return (
    <div className="page-form">
      <div className="space-y-3 border border-primary/30 bg-card p-6">
        <Skeleton className="h-5 w-24" />
        <Skeleton className="h-7 w-1/2" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-4/5" />
      </div>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 8 }).map((_, i) => (
          <div key={i} className="space-y-2 border border-border bg-card p-4">
            <Skeleton className="h-3 w-20" />
            <Skeleton className="h-6 w-24" />
            <Skeleton className="h-3 w-28" />
          </div>
        ))}
      </div>
      <div className="flex flex-wrap gap-2">
        <Skeleton className="h-9 w-32" />
        <Skeleton className="h-9 w-32" />
        <Skeleton className="h-9 w-32" />
      </div>
      <div className="space-y-4 border border-border bg-card p-6">
        <Skeleton className="h-5 w-1/3" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-11/12" />
        <Skeleton className="h-48 w-full" />
      </div>
    </div>
  )
}

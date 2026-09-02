import { createFileRoute, Link } from '@tanstack/react-router'
import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, ArrowRight } from 'lucide-react'
import { PageHeader } from '@/components/page-header'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { toJsonBytes } from '@/lib/codec'
import { dataApi, isUnimplemented, rpcErrorMessage, studyApi } from '@/lib/rpc'
import { session, useSession } from '@/lib/store'
import { describeLocally, describeNode } from '@/lib/describe'
import type { LocalDescription } from '@/lib/describe'
import {
  displaySymbol,
  displayTimeframe,
  expressionNodeIDs,
  hasEntry,
  LADDER_SLOTS,
  nodeWindowOf,
  parseStrategy,
  timeframesAtOrAbove,
  toEngineStrategy,
  withEntrySide,
  withNodeParams,
  withNodeTimeframe,
  withNodeWindow,
  withStarterEntry,
} from '@/lib/strategy'
import type { LadderSlot, StrategyDoc, StrategyNode } from '@/lib/strategy'
import { barsToDuration, displayDuration, durationToBars } from '@/lib/duration'
import {
  isTimeframeSupportedForInstrument,
  sortTimeframes,
  supportedTimeframesForInstrument,
} from '@/lib/constraints'

export const Route = createFileRoute('/strategy')({ component: DefineRules })

const LADDER_LABELS: Record<LadderSlot, string> = {
  bias: 'Bias',
  poi: 'POI',
  entry: 'Entry',
}

interface FieldError {
  path: string
  message: string
}

interface Instrument {
  symbol: string
  timeframes: string[]
}

function DefineRules() {
  const s = useSession()
  const document = useMemo(
    () => parseStrategy(s.strategyText),
    [s.strategyText],
  )

  const [errors, setErrors] = useState<FieldError[]>([])
  const [described, setDescribed] = useState<LocalDescription | null>(null)
  const [describeFallback, setDescribeFallback] = useState(false)
  const [requestError, setRequestError] = useState<string | null>(null)
  const [instruments, setInstruments] = useState<Instrument[]>([])

  useEffect(() => {
    void dataApi
      .listInstruments({})
      .then((response) =>
        setInstruments(
          response.instruments.map((i) => ({
            symbol: i.symbol,
            timeframes: i.timeframes,
          })),
        ),
      )
      .catch(() => setInstruments([]))
  }, [])

  // Validation and the plain-English reading both belong to the server: they
  // are derived from the document the engine will actually run. The local
  // description below is a stand-in while the request is in flight, and is
  // labelled as one.
  useEffect(() => {
    if (!document) {
      setDescribed(null)
      return
    }
    const local = describeLocally(document)
    setDescribed(local)

    const timer = window.setTimeout(() => {
      let strategyJson: Uint8Array
      try {
        strategyJson = toJsonBytes(toEngineStrategy(document))
      } catch (cause) {
        setErrors([])
        setRequestError(rpcErrorMessage(cause))
        session.set({ validatedHash: null })
        return
      }

      void studyApi
        .validate({ strategyJson })
        .then((validation) => {
          setRequestError(null)
          setErrors(validation.errors)
          session.set({
            validatedHash: validation.errors.length ? null : validation.hash,
          })
        })
        .catch((cause: unknown) => setRequestError(rpcErrorMessage(cause)))

      void studyApi
        .describe({ strategyJson })
        .then((response) => {
          setDescribeFallback(false)
          setDescribed({
            summary: response.description?.summary ?? local.summary,
            rules: response.description?.rules ?? [],
          })
        })
        .catch((cause: unknown) => {
          // An older server has no Describe. That is not the user's problem
          // and must not take the validation result down with it.
          setDescribeFallback(isUnimplemented(cause))
        })
    }, 350)

    return () => window.clearTimeout(timer)
  }, [document])

  if (!document) {
    return (
      <div className="space-y-6">
        <PageHeader step={2} title="Define the rules" />
        <Alert variant="destructive">
          <AlertTriangle className="size-4" />
          <AlertDescription>
            The strategy document is not valid JSON, so it cannot be edited
            here. Fix it in the advanced view, or start again from a template.
          </AlertDescription>
        </Alert>
        <div className="flex gap-2">
          <Button variant="outline" asChild>
            <Link to="/">Back to starting points</Link>
          </Button>
        </div>
      </div>
    )
  }

  const update = (next: StrategyDoc) =>
    session.set({
      strategyText: JSON.stringify(next, null, 2),
      validatedHash: null,
    })

  const errorFor = (path: string) =>
    errors.find((e) => e.path === path || e.path.startsWith(`${path}.`))
      ?.message

  const symbol = document.universe.symbols[0] ?? ''
  const selected = instruments.find((i) => i.symbol === symbol)
  const timeframes = selected
    ? sortTimeframes(
        supportedTimeframesForInstrument(symbol, selected.timeframes),
      )
    : []

  const nodesFor = (ids: string[]) =>
    ids
      .map((id) => document.nodes.find((n) => n.id === id))
      .filter((n): n is StrategyNode => n !== undefined)

  const conditionCount = Number(hasEntry(document))

  return (
    <div className="space-y-6">
      <PageHeader
        step={2}
        title="Define the rules"
        description="Each row is one decision. The reading underneath is generated from the same document the engine runs, so what it says and what gets tested cannot drift apart."
      />

      {requestError ? (
        <Alert variant="destructive">
          <AlertTriangle className="size-4" />
          <AlertDescription>
            Could not reach the local server to check these rules:{' '}
            {requestError}
          </AlertDescription>
        </Alert>
      ) : null}

      <Tabs defaultValue="guided">
        <TabsList>
          <TabsTrigger value="guided">Guided</TabsTrigger>
          <TabsTrigger value="advanced">Advanced</TabsTrigger>
        </TabsList>

        <TabsContent value="guided" className="space-y-8 pt-4">
          <section className="border-t border-border">
            <Row label="Market" error={errorFor('universe.symbols')}>
              <select
                className="h-9 border border-input bg-card pl-3 pr-9 text-sm"
                value={symbol}
                onChange={(e) => {
                  const next = e.target.value
                  const instrument = instruments.find((i) => i.symbol === next)
                  const supported = instrument
                    ? sortTimeframes(
                        supportedTimeframesForInstrument(
                          next,
                          instrument.timeframes,
                        ),
                      )
                    : []
                  const timeframe = supported.includes(
                    document.timeframes.entry,
                  )
                    ? document.timeframes.entry
                    : (supported[0] ?? '')
                  // setMarket rewrites the document and the session selection
                  // together; doing them separately is how the two drift.
                  session.setMarket(next, timeframe)
                }}
              >
                <option value="">Select a market…</option>
                {instruments.map((i) => (
                  <option key={i.symbol} value={i.symbol}>
                    {displaySymbol(i.symbol)}
                  </option>
                ))}
              </select>
              {instruments.length === 0 ? (
                <span className="font-mono text-[11px] text-muted-foreground">
                  no instruments returned — is the local server running?
                </span>
              ) : null}
            </Row>

            <Row
              label="Analysis ladder"
              error={
                errorFor('timeframes.entry') ??
                errorFor('timeframes.poi') ??
                errorFor('timeframes.bias')
              }
            >
              <div className="flex w-full flex-col gap-3">
                {timeframes.length === 0 ? (
                  <span className="font-mono text-[11px] text-muted-foreground">
                    select a market first
                  </span>
                ) : (
                  (LADDER_SLOTS as readonly LadderSlot[]).map((slot) => (
                    <div
                      key={slot}
                      className="flex flex-wrap items-center gap-2"
                    >
                      <span className="w-14 font-mono text-[11px] uppercase tracking-wider text-muted-foreground">
                        {LADDER_LABELS[slot]}
                      </span>
                      <div className="flex flex-wrap gap-1">
                        {timeframes.map((tf) => (
                          <Choice
                            key={tf}
                            active={document.timeframes[slot] === tf}
                            onClick={() => session.setSlotTimeframe(slot, tf)}
                          >
                            {displayTimeframe(tf)}
                          </Choice>
                        ))}
                      </div>
                    </div>
                  ))
                )}
                <span className="text-sm text-muted-foreground">
                  Bias sits at the top and entry at the bottom; each rung must
                  be the same or finer than the one above it. Entry is the chart
                  the run loads — coarser rungs are aggregated from it and read
                  at their close. Leave all three the same for a
                  single-timeframe study.
                </span>
                {symbol &&
                document.timeframes.entry &&
                !isTimeframeSupportedForInstrument(
                  symbol,
                  document.timeframes.entry,
                ) ? (
                  <span className="font-mono text-[11px] text-warning">
                    this source cannot prepare the entry resolution
                  </span>
                ) : null}
              </div>
            </Row>

            <Row label="Test direction">
              <div
                className="flex flex-wrap gap-1"
                role="group"
                aria-label="Test direction"
              >
                {(['long', 'short'] as const).map((side) => (
                  <Choice
                    key={side}
                    active={document.entries[side].enabled}
                    onClick={() => update(withEntrySide(document, side))}
                  >
                    {side === 'long' ? 'Long' : 'Short'}
                  </Choice>
                ))}
              </div>
              <span className="text-sm text-muted-foreground">
                One side runs at a time. Switching side keeps the same entry
                condition and changes the position taken when it fires.
              </span>
            </Row>
          </section>

          {(['long', 'short'] as const).map((side) => {
            const entry = document.entries[side]
            const nodes = nodesFor(expressionNodeIDs(entry.when))
            if (!entry.enabled || nodes.length === 0) return null
            return (
              <section
                key={side}
                className="space-y-1 border-t border-border pt-4"
              >
                <h2 className="text-base font-semibold">
                  {side === 'long' ? 'Long entry' : 'Short entry'}
                </h2>
                <p className="text-sm text-muted-foreground">{entry.name}</p>
                <div className="mt-2">
                  {nodes.map((node) => (
                    <ConditionRow
                      key={node.id}
                      node={node}
                      entryTimeframe={document.timeframes.entry}
                      timeframeError={errorFor(
                        `nodes[${document.nodes.indexOf(node)}].timeframe`,
                      )}
                      heldFor={nodeWindowOf(entry.when, node.id)}
                      onChange={(params) =>
                        update(withNodeParams(document, node.id, params))
                      }
                      onTimeframe={(tf) =>
                        update(withNodeTimeframe(document, node.id, tf))
                      }
                      onHeldFor={(window) =>
                        update(withNodeWindow(document, side, node.id, window))
                      }
                    />
                  ))}
                </div>
              </section>
            )
          })}

          {conditionCount === 0 ? (
            <Alert variant="destructive">
              <AlertTriangle className="size-4" />
              <AlertDescription>
                <p>
                  This document has no entry condition, so nothing would ever
                  trigger.
                </p>
                <Button
                  className="mt-3"
                  size="sm"
                  variant="outline"
                  onClick={() => update(withStarterEntry(document))}
                >
                  Add a 20/50 crossover entry
                </Button>
                <p className="mt-2 text-sm">
                  Or start from a template if you want a different kind of rule.
                </p>
              </AlertDescription>
            </Alert>
          ) : null}

          <section className="space-y-1 border-t border-border pt-4">
            <h2 className="text-base font-semibold">Stop and target</h2>
            <p className="text-sm text-muted-foreground">
              Both carry a unit. A bare number means something different the
              moment the market changes, so the engine refuses to accept one.
            </p>
            <div className="mt-2">
              <Row label="Stop" error={errorFor('risk.stop')}>
                <DistanceInput
                  distance={document.risk.stop}
                  onChange={(stop) =>
                    update({ ...document, risk: { ...document.risk, stop } })
                  }
                />
              </Row>
              <Row label="Target" error={errorFor('risk.target')}>
                <DistanceInput
                  distance={document.risk.target}
                  onChange={(target) =>
                    update({ ...document, risk: { ...document.risk, target } })
                  }
                />
              </Row>
              <Row label="Entry timing">
                <span className="text-sm">Next bar open</span>
                <span className="font-mono text-[11px] text-muted-foreground">
                  the only fill policy the simulator executes
                </span>
              </Row>
            </div>
          </section>

          <section className="border border-border bg-card p-5">
            <p className="runlog-label">What this says in plain English</p>
            <p className="mt-2 text-[17px] leading-relaxed">
              {described?.summary ?? 'Reading your rules…'}
            </p>
            {described?.rules.length ? (
              <ul className="mt-3 space-y-1 border-t border-border pt-3">
                {described.rules
                  .filter((rule) => rule.enabled)
                  .map((rule) => (
                    <li
                      key={rule.branch}
                      className="text-sm text-muted-foreground"
                    >
                      <span className="font-mono text-[11px] uppercase tracking-wider">
                        {rule.branch}
                      </span>{' '}
                      {rule.text}
                    </li>
                  ))}
              </ul>
            ) : null}
            {describeFallback ? (
              <p className="mt-3 font-mono text-[11px] text-muted-foreground">
                written by this client — the local server does not generate
                readings yet, so rebuild it to have this come from the engine
              </p>
            ) : null}
          </section>

          {errors.length ? (
            <Alert variant="destructive">
              <AlertTriangle className="size-4" />
              <AlertDescription>
                <p className="font-medium">This cannot run yet:</p>
                <ul className="mt-1 space-y-1">
                  {errors.map((error) => (
                    <li key={error.path} className="text-sm">
                      <span className="font-mono text-[11px]">
                        {error.path}
                      </span>{' '}
                      — {error.message}
                    </li>
                  ))}
                </ul>
              </AlertDescription>
            </Alert>
          ) : null}
        </TabsContent>

        <TabsContent value="advanced" className="space-y-3 pt-4">
          <p className="text-sm text-muted-foreground">
            The document exactly as it will be sent. Everything above edits
            this; nothing is stored anywhere else.
          </p>
          <Textarea
            className="min-h-[460px] font-mono text-xs"
            spellCheck={false}
            value={s.strategyText}
            onChange={(e) =>
              session.set({ strategyText: e.target.value, validatedHash: null })
            }
          />
        </TabsContent>
      </Tabs>

      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-5">
        <Button variant="outline" asChild>
          <Link to="/">Back to starting points</Link>
        </Button>
        <div className="flex flex-wrap items-center justify-end gap-3">
          <span className="font-mono text-[11px] text-muted-foreground">
            {s.validatedHash
              ? `validated · ${s.validatedHash.slice(0, 10)}…`
              : 'waiting for the server to accept these rules'}
          </span>
          <Button asChild disabled={!s.validatedHash}>
            <Link to="/reality">
              Continue to trading reality
              <ArrowRight className="size-4" />
            </Link>
          </Button>
        </div>
      </div>
    </div>
  )
}

function Row({
  label,
  error,
  children,
}: {
  label: string
  error?: string
  children: React.ReactNode
}) {
  return (
    <div className="grid items-baseline gap-x-5 gap-y-1 border-b border-border/60 last:border-b-0 py-3 sm:grid-cols-[170px_minmax(0,1fr)]">
      <Label className="runlog-label">{label}</Label>
      <div className="flex flex-wrap items-center gap-3">{children}</div>
      {error ? (
        <p className="text-sm text-destructive sm:col-start-2">{error}</p>
      ) : null}
    </div>
  )
}

function Choice({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={
        active
          ? 'border border-primary bg-primary px-3 py-1.5 text-sm text-primary-foreground'
          : 'border border-input bg-card px-3 py-1.5 text-sm hover:bg-accent'
      }
    >
      {children}
    </button>
  )
}

/** Turn "fastPeriod" into "fast period" without pretending to know more. */
function humanise(key: string): string {
  return key
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/_/g, ' ')
    .toLowerCase()
}

function ConditionRow({
  node,
  entryTimeframe,
  timeframeError,
  heldFor,
  onChange,
  onTimeframe,
  onHeldFor,
}: {
  node: StrategyNode
  entryTimeframe: string
  timeframeError?: string
  heldFor: string | null
  onChange: (params: Record<string, unknown>) => void
  onTimeframe: (tf: string) => void
  onHeldFor: (window: string | null) => void
}) {
  const numeric = Object.entries(node.params).filter(
    ([, value]) => typeof value === 'number',
  ) as Array<[string, number]>
  // A node runs on its own timeframe, defaulting to the entry rung. The window
  // is authored in bars of that timeframe and stored as the duration the engine
  // reads, so "3 bars of h4" and "12h" are the same window under either lens.
  const tf = node.timeframe || entryTimeframe
  const options = timeframesAtOrAbove(entryTimeframe)
  const heldForBars = heldFor ? durationToBars(heldFor, tf) : null

  return (
    <div className="grid items-baseline gap-x-5 gap-y-2 border-b border-border/60 last:border-b-0 py-3 sm:grid-cols-[170px_minmax(0,1fr)]">
      <span className="font-mono text-[11px] text-muted-foreground">
        {node.pack}/{node.type}
      </span>
      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-4">
          <p className="text-sm">{describeNode(node)}</p>
          {numeric.map(([key, value]) => (
            <label key={key} className="flex items-center gap-2">
              <span className="font-mono text-[11px] text-muted-foreground">
                {humanise(key)}
              </span>
              <Input
                type="number"
                className="h-8 w-24 font-mono text-xs"
                value={value}
                onChange={(e) => onChange({ [key]: Number(e.target.value) })}
              />
            </label>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-4">
          <label className="flex items-center gap-2">
            <span className="font-mono text-[11px] text-muted-foreground">
              timeframe
            </span>
            <select
              className="h-8 border border-input bg-card pl-2 pr-7 text-xs"
              value={tf}
              onChange={(e) => onTimeframe(e.target.value)}
            >
              {options.map((option) => (
                <option key={option} value={option}>
                  {displayTimeframe(option)}
                </option>
              ))}
            </select>
          </label>
          <label className="flex items-center gap-2">
            <span className="font-mono text-[11px] text-muted-foreground">
              stays live for
            </span>
            <Input
              type="number"
              min="0"
              placeholder="—"
              className="h-8 w-20 font-mono text-xs"
              value={heldForBars ?? ''}
              onChange={(e) => {
                const n = Number(e.target.value)
                onHeldFor(
                  Number.isFinite(n) && n > 0 ? barsToDuration(n, tf) : null,
                )
              }}
            />
            <span className="font-mono text-[11px] text-muted-foreground">
              {tf}-bars{heldFor ? ` · ${displayDuration(heldFor)}` : ''}
            </span>
          </label>
        </div>
        {timeframeError ? (
          <p className="text-sm text-destructive">{timeframeError}</p>
        ) : null}
      </div>
    </div>
  )
}

function DistanceInput({
  distance,
  onChange,
}: {
  distance: StrategyDoc['risk']['stop']
  onChange: (next: StrategyDoc['risk']['stop']) => void
}) {
  return (
    <div className="flex items-center gap-2">
      <Input
        type="number"
        step="0.1"
        min="0"
        className="h-9 w-28 font-mono text-sm"
        value={distance.value}
        onChange={(e) =>
          onChange({ ...distance, value: Number(e.target.value) })
        }
      />
      <select
        className="h-9 border border-input bg-card pl-3 pr-9 text-sm"
        value={distance.unit}
        onChange={(e) =>
          onChange({
            ...distance,
            unit: e.target.value as StrategyDoc['risk']['stop']['unit'],
          })
        }
      >
        <option value="pips">pips</option>
        <option value="ticks">ticks</option>
        <option value="percent">percent</option>
        <option value="atr">× ATR</option>
        <option value="price">price</option>
      </select>
    </div>
  )
}

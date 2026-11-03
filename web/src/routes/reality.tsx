import { createFileRoute, Link } from '@tanstack/react-router'
import { ArrowRight, Info } from 'lucide-react'
import { useEffect, useState } from 'react'
import { PageHeader } from '@/components/page-header'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { session, useSession } from '@/lib/store'
import type { StudyAssumptionsState } from '@/lib/store'
import { centsToUsdInput, formatUsdCents, usdInputToCents } from '@/lib/codec'

export const Route = createFileRoute('/reality')({ component: TradingReality })

/** The three presets. Custom is whatever the user last typed. */
const PRESETS = {
  typical: {
    spreadTicks: 0n,
    commissionBps: 0,
    commissionFlat: 0n,
    slippageTicks: 0n,
  },
  conservative: {
    spreadTicks: 2n,
    commissionBps: 0,
    commissionFlat: 0n,
    slippageTicks: 2n,
  },
} as const

function TradingReality() {
  const { assumptions } = useSession()
  const update = (patch: Partial<StudyAssumptionsState>) =>
    session.set({ assumptions: { ...assumptions, ...patch } })

  const overridden =
    assumptions.spreadTicks > 0n ||
    assumptions.slippageTicks > 0n ||
    assumptions.commissionBps > 0 ||
    assumptions.commissionFlat > 0n

  return (
    <div className="page-form">
      <PageHeader
        step={3}
        title="Set trading reality"
        description="What the account looked like and what each round turn cost. These decide whether an edge is real, so they travel with the run and are shown again on the result."
      />

      <Alert className="notice">
        <Info className="size-4" />
        <AlertDescription>
          Leaving a cost at zero does not mean free execution. It means “use the
          instrument's own cost”, which is the only reading that cannot quietly
          flatter a strategy. There is no combination of inputs here that
          produces a zero-cost backtest.
        </AlertDescription>
      </Alert>

      <section className="panel">
        <div className="rows">
          <Row label="Cost preset" hint="applied to the fields below">
            <div className="flex flex-wrap gap-2">
              <Choice
                active={!overridden}
                onClick={() => update(PRESETS.typical)}
              >
                Typical
              </Choice>
              <Choice
                active={
                  assumptions.spreadTicks ===
                    PRESETS.conservative.spreadTicks &&
                  assumptions.slippageTicks ===
                    PRESETS.conservative.slippageTicks
                }
                onClick={() => update(PRESETS.conservative)}
              >
                Conservative
              </Choice>
            </div>
          </Row>
        </div>
      </section>

      <section className="panel">
        <div className="panel-head">
          <h2>Account and position size</h2>
          <p>
            Risk per trade sizes each position from current balance. Use a
            non-zero value to compound the account; fixed lots are for
            deliberately chosen exposure.
          </p>
        </div>
        <div className="rows">
          <Row label="Starting balance" hint="USD; stored and sent as cents">
            <Input
              type="number"
              min="1"
              aria-label="Starting balance (USD)"
              className="h-9 w-40 font-mono text-sm"
              value={centsToUsdInput(assumptions.initialEquity)}
              onChange={(e) => {
                const cents = usdInputToCents(e.target.value)
                if (cents !== null) update({ initialEquity: cents })
              }}
            />
            <span className="row-assist">
              {formatUsdCents(assumptions.initialEquity)}
            </span>
          </Row>
          <Row label="Risk per trade" hint="percent of the account">
            <RiskInput
              value={assumptions.riskPerTrade}
              onChange={(riskPerTrade) => update({ riskPerTrade })}
            />
            <span className="row-assist">
              {formatUsdCents(
                BigInt(
                  Math.round(
                    Number(assumptions.initialEquity) *
                      assumptions.riskPerTrade,
                  ),
                ),
              )}{' '}
              at risk on the first trade
            </span>
          </Row>
          <Row
            label="Fixed lots"
            hint={
              assumptions.riskPerTrade > 0
                ? 'ignored while risk per trade is set'
                : undefined
            }
          >
            <Input
              type="number"
              min="0"
              step="0.01"
              disabled={assumptions.riskPerTrade > 0}
              aria-label="Fixed lots"
              className="h-9 w-40 font-mono text-sm"
              value={assumptions.fixedLots}
              onChange={(e) => update({ fixedLots: Number(e.target.value) })}
            />
          </Row>
        </div>
      </section>

      <section className="panel">
        <div className="panel-head">
          <h2>Costs and fills</h2>
          <p>
            Each field overrides the instrument's own cost only when it is above
            zero.
          </p>
        </div>
        <div className="rows">
          <Row label="Spread override" hint="ticks">
            <Input
              type="number"
              min="0"
              aria-label="Spread override (ticks)"
              className="h-9 w-40 font-mono text-sm"
              value={assumptions.spreadTicks.toString()}
              onChange={(e) =>
                update({ spreadTicks: BigInt(e.target.value || 0) })
              }
            />
          </Row>
          <Row label="Commission" hint="basis points">
            <Input
              type="number"
              min="0"
              aria-label="Commission (basis points)"
              className="h-9 w-40 font-mono text-sm"
              value={assumptions.commissionBps}
              onChange={(e) =>
                update({ commissionBps: Number(e.target.value) })
              }
            />
          </Row>
          <Row label="Flat commission" hint="USD per round turn; sent as cents">
            <Input
              type="number"
              min="0"
              step="0.01"
              aria-label="Flat commission (USD per round turn)"
              className="h-9 w-40 font-mono text-sm"
              value={centsToUsdInput(assumptions.commissionFlat)}
              onChange={(e) => {
                const cents = usdInputToCents(e.target.value)
                if (cents !== null) update({ commissionFlat: cents })
              }}
            />
            <span className="row-assist">
              {formatUsdCents(assumptions.commissionFlat)}
            </span>
          </Row>
          <Row label="Slippage override" hint="ticks, both sides">
            <Input
              type="number"
              min="0"
              aria-label="Slippage override (ticks)"
              className="h-9 w-40 font-mono text-sm"
              value={assumptions.slippageTicks.toString()}
              onChange={(e) =>
                update({ slippageTicks: BigInt(e.target.value || 0) })
              }
            />
          </Row>
          <Row label="Fill policy" hint="the only executable policy">
            <span className="text-sm">Next bar open</span>
          </Row>
          <Row
            label="Ambiguous fills"
            hint="depends on the chosen market, timeframe, and date range"
          >
            <span className="text-sm">
              When one bar touches both the stop and the target, the data cannot
              show which came first. Any such trades are flagged on the result.
            </span>
          </Row>
        </div>
      </section>

      <div className="page-footer">
        <Button variant="outline" asChild>
          <Link to="/strategy">Back to rules</Link>
        </Button>
        <div className="page-footer-right">
          <span className="footer-status">
            {overridden
              ? 'using your cost overrides'
              : 'using each instrument’s own costs'}
          </span>
          <Button asChild>
            <Link
              to="/run"
              onClick={() => session.set({ realityConfirmed: true })}
            >
              Continue to the test
              <ArrowRight className="size-4" />
            </Link>
          </Button>
        </div>
      </div>
    </div>
  )
}

/**
 * Risk is authored in percent because that is how traders talk about it, but
 * stored as the fraction the engine reads. The draft text is kept separately
 * so typing "0.5" survives the half-typed "0." that a controlled number input
 * would otherwise swallow.
 */
function RiskInput({
  value,
  onChange,
}: {
  value: number
  onChange: (value: number) => void
}) {
  const [text, setText] = useState(() => percentText(value))
  // Only a change from outside this field (a preset or a new session) needs to
  // overwrite the draft; keystrokes already agree with the stored value.
  useEffect(() => {
    if (Math.abs(Number(text) / 100 - value) > 1e-9) setText(percentText(value))
  }, [value])
  return (
    <Input
      type="number"
      min="0"
      max="100"
      step="0.25"
      aria-label="Risk per trade (percent of the account)"
      className="h-9 w-40 font-mono text-sm"
      value={text}
      onChange={(e) => {
        setText(e.target.value)
        const percent = Number(e.target.value)
        if (e.target.value.trim() === '' || !Number.isFinite(percent)) return
        onChange(Math.min(1, Math.max(0, percent / 100)))
      }}
      onBlur={() => setText(percentText(value))}
    />
  )
}

function percentText(fraction: number) {
  return String(Number((fraction * 100).toFixed(2)))
}

function Row({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <div className="row">
      <Label className="runlog-label">{label}</Label>
      <div className="row-controls">{children}</div>
      {hint ? <p className="row-hint">{hint}</p> : null}
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
      className="choice"
    >
      {children}
    </button>
  )
}

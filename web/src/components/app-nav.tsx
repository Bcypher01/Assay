import { Link } from '@tanstack/react-router'
import {
  CalendarClock,
  Check,
  Minus,
  PencilRuler,
  RotateCcw,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { hasReadyData, session as sessionStore, useSession } from '@/lib/store'
import type { SessionState } from '@/lib/store'
import { displaySymbol, displayTimeframe, parseStrategy } from '@/lib/strategy'
import { formatUsdCents } from '@/lib/codec'

/**
 * The run log.
 *
 * This is the whole navigation model: one spine that accumulates what has been
 * decided, in order, with each step's own summary underneath it. It is a
 * wayfinder and an audit trail at once — a user can see at a glance that they
 * are testing EUR/USD on the 4-hour chart at 1% risk without opening three
 * screens to check, and the trial count sits at the bottom where it cannot be
 * scrolled away from.
 */

type StepPath = '/' | '/strategy' | '/reality' | '/data' | '/run' | '/history'

interface Step {
  to: StepPath
  label: string
  /** One line describing what has been decided here, or null when nothing has. */
  summary: (s: SessionState) => string | null
  /** Whether this step's decision is settled. */
  done: (s: SessionState) => boolean
}

const steps: Step[] = [
  {
    to: '/',
    label: 'Choose a starting point',
    summary: (s) => {
      if (!s.templateId) return null
      const document = parseStrategy(s.strategyText)
      if (!document || document.nodes.length === 0) return null
      return document.name
    },
    done: (s) => !!s.templateId,
  },
  {
    to: '/strategy',
    label: 'Define the rules',
    summary: (s) => {
      const document = parseStrategy(s.strategyText)
      if (!document) return 'document is not valid JSON'
      const symbol = document.universe.symbols[0]
      if (!symbol || !document.timeframes.entry) return null
      const sides = [document.entries.long, document.entries.short]
        .map((entry, index) =>
          entry.enabled ? (index === 0 ? 'long' : 'short') : null,
        )
        .filter(Boolean)
        .join(' + ')
      return `${displaySymbol(symbol)} · ${displayTimeframe(document.timeframes.entry)} · ${sides || 'no entries'}`
    },
    done: (s) => !!s.validatedHash,
  },
  {
    to: '/reality',
    label: 'Set trading reality',
    summary: (s) => {
      if (!s.realityConfirmed) return null
      const { riskPerTrade, initialEquity, slippageTicks } = s.assumptions
      const sizing =
        riskPerTrade > 0
          ? `${(riskPerTrade * 100).toFixed(2)}% risk`
          : `${s.assumptions.fixedLots} lots`
      const slip =
        slippageTicks > 0n
          ? `${slippageTicks} tick slippage`
          : 'instrument slippage'
      return `${formatUsdCents(initialEquity)} account · ${sizing} · ${slip}`
    },
    done: (s) => s.realityConfirmed,
  },
  {
    to: '/data',
    label: 'Prepare the history',
    summary: (s) => {
      if (!s.symbol || !s.timeframe) return null
      if (!s.fromMs || !s.toMs)
        return `${displaySymbol(s.symbol)} ${s.timeframe}, no window yet`
      const years = Number(s.toMs - s.fromMs) / (365.25 * 24 * 60 * 60 * 1000)
      return `${years.toFixed(1)} years for ${displaySymbol(s.symbol)} ${s.timeframe}`
    },
    done: (s) => hasReadyData(s),
  },
  {
    to: '/run',
    label: 'Run the test',
    summary: (s) => (s.lastRunId ? `run ${s.lastRunId.slice(0, 10)}…` : null),
    done: (s) => !!s.lastRunId,
  },
  {
    to: '/history',
    label: 'Read the verdict',
    summary: (s) =>
      s.trials > 0
        ? `${s.trials} ${s.trials === 1 ? 'run' : 'runs'} this session`
        : null,
    done: (s) => !!s.lastRunId,
  },
]

/**
 * The verdict step goes straight to the latest result once there is one.
 * Sending someone to a list of runs to find the run they just did is a step
 * the product does not need.
 */
function destination(step: Step, session: SessionState) {
  if (step.to === '/history' && session.lastRunId) {
    return { to: '/runs/$runId' as const, params: { runId: session.lastRunId } }
  }
  return { to: step.to }
}

export function AppNav() {
  const session = useSession()

  return (
    <aside className="runlog-spine">
      <Link to="/" className="runlog-wordmark" aria-label="Assay — run log">
        {/* The mark is the A. The word continues out of it rather than
            restating it, so the lockup reads as one word, not a badge
            standing next to its own initial. */}
        <span className="runlog-mark" aria-hidden="true">
          A
        </span>
        <span className="runlog-wordmark-rest" aria-hidden="true">
          ssay
        </span>
        <small aria-hidden="true">Run log</small>
      </Link>

      <nav className="runlog-nav">
        {steps.map((step, index) => {
          const done = step.done(session)
          const summary = step.summary(session)
          const unlocked = index === 0 || steps[index - 1].done(session)
          // A step that has a decision on record but is not currently settled
          // reads as "recorded, needs re-checking" rather than reverting to
          // blank. Validation is deliberately dropped on reload and on every
          // edit, and a tick that silently disappears looks like lost work.
          const started = unlocked && !done && summary !== null
          return (
            <Link
              key={step.to}
              {...destination(step, session)}
              aria-disabled={!unlocked}
              tabIndex={unlocked ? undefined : -1}
              onClick={(event) => {
                if (!unlocked) event.preventDefault()
              }}
              className={cn(
                'runlog-entry',
                !unlocked && 'pointer-events-none opacity-50',
              )}
              activeOptions={{ exact: step.to === '/' }}
              activeProps={{ 'data-status': 'active' }}
            >
              <span
                className={cn(
                  'runlog-marker',
                  done && 'runlog-marker-done',
                  started && 'runlog-marker-started',
                )}
                title={
                  done
                    ? 'Settled'
                    : started
                      ? 'Recorded — needs re-checking'
                      : !unlocked
                        ? 'Complete the previous step first'
                        : undefined
                }
              >
                {done ? <Check /> : started ? <Minus /> : null}
              </span>
              <span className="runlog-entry-copy">
                <small>Step {index + 1}</small>
                {step.label}
                {summary ? <em>{summary}</em> : null}
              </span>
            </Link>
          )
        })}
      </nav>

      <div className="runlog-tools">
        <p className="runlog-label">Tools</p>
        <Link
          to="/setup"
          className="runlog-tool"
          activeProps={{ 'data-status': 'active' }}
        >
          <PencilRuler aria-hidden="true" />
          <span>Setup test</span>
        </Link>
        <Link
          to="/events"
          className="runlog-tool"
          activeProps={{ 'data-status': 'active' }}
        >
          <CalendarClock aria-hidden="true" />
          <span>Event study</span>
        </Link>
      </div>

      <section className="runlog-session" aria-label="Current research session">
        <div className="runlog-session-heading">
          <div>
            <p className="runlog-label">Research trail</p>
            <h2>Trials this session</h2>
          </div>
          <strong aria-label={`${session.trials} trials`}>
            {session.trials}
          </strong>
        </div>
        <p className="runlog-session-copy">
          Every completed test counts, including repeats, so the result keeps
          its full research history.
        </p>
        <Button
          className="runlog-reset"
          variant="outline"
          onClick={() => sessionStore.reset()}
        >
          <RotateCcw className="size-4" />
          <span className="runlog-reset-wide">Start a new session</span>
          <span className="runlog-reset-compact">New session</span>
        </Button>
      </section>

      <div className="runlog-note">
        <p>
          A backtest describes what a rule did on one historical sample. It does
          not describe what it will do.
        </p>
      </div>
    </aside>
  )
}

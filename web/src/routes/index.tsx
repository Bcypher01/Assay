import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import {
  ArrowRight,
  CandlestickChart,
  Info,
  Layers,
  Milestone,
  TrendingUp,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { loadTemplates } from '@/lib/templates'
import type { TemplateCard } from '@/lib/templates'
import { displaySymbol, displayTimeframe, withEntrySide } from '@/lib/strategy'
import { session, useSession } from '@/lib/store'
import { assayHttpBaseUrl, rpcErrorMessage } from '@/lib/rpc'
import { loadOnboarding } from '@/lib/onboarding'

export const Route = createFileRoute('/')({ component: StartingPoint })

function StartingPoint() {
  const navigate = useNavigate()
  const sessionState = useSession()
  const [templates, setTemplates] = useState<TemplateCard[]>([])
  const [notice, setNotice] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    void (async () => {
      // A restored or intentionally selected study always wins. Otherwise the
      // server's worked four-year example is the first editable document.
      if (!sessionState.onboardingSeen && !sessionState.templateId) {
        try {
          const onboarding = await loadOnboarding(assayHttpBaseUrl)
          if (cancelled) return
          session.set({
            strategyText: JSON.stringify(
              withEntrySide(onboarding.document, 'long'),
              null,
              2,
            ),
            templateId: 'onboarding',
            symbol: onboarding.symbol,
            timeframe: onboarding.timeframe,
            fromMs: onboarding.fromMs,
            toMs: onboarding.toMs,
            validatedHash: null,
            realityConfirmed: false,
            onboardingSeen: true,
          })
          await navigate({ to: '/strategy' })
          return
        } catch (error) {
          if (!cancelled)
            setNotice(
              `Couldn’t load the server’s worked example: ${rpcErrorMessage(error)}`,
            )
        }
        session.set({ onboardingSeen: true })
      }
      const catalogue = await loadTemplates()
      if (cancelled) return
      setTemplates(catalogue.templates)
      setNotice((previous) => previous ?? catalogue.notice)
      setLoading(false)
    })()
    return () => {
      cancelled = true
    }
  }, [navigate, sessionState.onboardingSeen, sessionState.templateId])

  function choose(template: TemplateCard) {
    const symbol = template.document.universe.symbols[0] ?? ''
    const timeframe = template.document.timeframes.entry
    session.set({
      // The server describes both branches, while the present runner executes
      // one direction per study. Start with Long selected explicitly.
      strategyText: JSON.stringify(
        withEntrySide(template.document, 'long'),
        null,
        2,
      ),
      templateId: template.id,
      validatedHash: null,
      symbol,
      timeframe,
    })
    void navigate({ to: '/strategy' })
  }

  return (
    <div className="rise">
      <section className="hero">
        <span className="hero-tag">What a rule did. Nothing more.</span>
        <h1>
          Test a rule against <em>real history.</em>
        </h1>
        <p>
          Pick a starting point below. Every rule here is one the engine can
          express in full — the strategy you read is the strategy that gets
          tested, right down to spread, slippage and the fills it could not
          order. Evidence about one sample is not a forecast.
        </p>
      </section>

      {notice ? (
        <Alert className="mt-6">
          <Info className="size-4" />
          <AlertDescription>{notice}</AlertDescription>
        </Alert>
      ) : null}

      <section>
        <div className="section-head">
          <div>
            <h2>Popular starting points</h2>
            <p>
              Pick one to open it in the editor. Nothing is locked — the market,
              timeframe, conditions and exits are all yours to change before
              anything runs.
            </p>
          </div>
        </div>

        {loading ? (
          <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="template">
                <div className="flex items-center justify-between">
                  <Skeleton className="h-5 w-20" />
                  <Skeleton className="h-3 w-24" />
                </div>
                <Skeleton className="h-5 w-3/4" />
                <div className="space-y-2">
                  <Skeleton className="h-3 w-full" />
                  <Skeleton className="h-3 w-5/6" />
                </div>
                <Skeleton className="h-12 w-full" />
                <Skeleton className="mt-auto h-3 w-40" />
                <Skeleton className="h-9 w-full" />
              </div>
            ))}
          </div>
        ) : (
          <div className="template-grid">
            {templates.map((template) => (
              <TemplateTile
                key={template.id}
                template={template}
                onChoose={choose}
              />
            ))}
          </div>
        )}
      </section>

      <section>
        <div className="section-head">
          <div>
            <h2>And when you’re ready</h2>
            <p>
              The vocabulary for both is already here; these are the doors still
              being built.
            </p>
          </div>
        </div>

        <div className="planned">
          <PlannedTile
            title="Describe my own strategy"
            body="Turn a written idea into exact, testable rules. The rule vocabulary is here now — a numeric comparison like “RSI below 40” is expressible today, as the RSI pullback template shows — so what is left is the translation itself: turning a sentence into the exact document, reliably enough that the rule you read is the rule that runs."
          />
          <PlannedTile
            title="Reopen a saved test"
            body="Return to an earlier run with its assumptions intact. Completed runs are already stored — this is the front door to them."
          />
        </div>
      </section>

      <p className="footnote">
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M12 20a8 8 0 0 0 8-8 8 8 0 0 0-8-8 8 8 0 0 0-8 8 8 8 0 0 0 8 8Z" />
          <path d="M12 8v4" />
          <path d="M12 16h.01" />
        </svg>
        <span>
          A backtest describes what a rule did on one historical sample. It does
          not describe what it will do — and every surface in Assay is built so
          that is hard to forget.
        </span>
      </p>
    </div>
  )
}

const FAMILY_ICONS: Record<string, LucideIcon> = {
  'Trend following': TrendingUp,
  'Price action': CandlestickChart,
  'Multi-timeframe': Layers,
  'SMC workflow': Milestone,
}

function TemplateTile({
  template,
  onChoose,
}: {
  template: TemplateCard
  onChoose: (template: TemplateCard) => void
}) {
  const symbol = template.document.universe.symbols[0] ?? ''
  const timeframe = template.document.timeframes.entry
  const FamilyIcon = FAMILY_ICONS[template.family] ?? null

  return (
    <article className="template">
      <div className="template-top">
        <span className="chip">
          {FamilyIcon ? <FamilyIcon aria-hidden="true" /> : null}
          {template.family}
        </span>
        <span className="tpl-freq">{template.expectedFrequency} frequency</span>
      </div>

      <h3>{template.name}</h3>
      <p className="tpl-desc">{template.description}</p>

      <p className="tpl-sig">{template.described.summary}</p>

      <p className="tpl-market">
        suggested: {displaySymbol(symbol)} · {displayTimeframe(timeframe)} ·
        long
      </p>

      <Button onClick={() => onChoose(template)}>
        Customise and test
        <ArrowRight className="size-4" />
      </Button>
    </article>
  )
}

function PlannedTile({ title, body }: { title: string; body: string }) {
  return (
    <article className="card">
      <span className="soon">Not built yet</span>
      <h3>{title}</h3>
      <p>{body}</p>
    </article>
  )
}

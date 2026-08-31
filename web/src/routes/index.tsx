import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { ArrowRight, Bookmark, Info, MessageSquare } from 'lucide-react'
import { PageHeader } from '@/components/page-header'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
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
    <div className="space-y-8">
      <PageHeader
        step={1}
        title="Choose a starting point"
        description="Every rule set below is one the engine can express in full, so the strategy you read on the card is the strategy that gets tested. A historical test is evidence about one sample, not a forecast."
      />

      {notice ? (
        <Alert>
          <Info className="size-4" />
          <AlertDescription>{notice}</AlertDescription>
        </Alert>
      ) : null}

      <section className="space-y-4">
        <div>
          <h2 className="text-lg font-semibold">Use a popular strategy</h2>
          <p className="text-sm text-muted-foreground">
            Pick one to open it in the editor. Nothing is locked — the market,
            timeframe, conditions and exits are all yours to change before
            anything runs.
          </p>
        </div>

        {loading ? (
          <div className="grid gap-px border border-border bg-border sm:grid-cols-2 xl:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="flex flex-col gap-3 bg-card p-5">
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
          <div className="grid gap-px border border-border bg-border sm:grid-cols-2 xl:grid-cols-3">
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

      <section className="grid gap-4 md:grid-cols-2">
        <PlannedTile
          icon={<MessageSquare className="size-4" />}
          title="Describe my own strategy"
          body="Turn a written idea into exact, testable rules. Waiting on the predicate layer: a condition like “RSI below 40” needs a numeric comparison, and a slot currently accepts boolean conditions only."
        />
        <PlannedTile
          icon={<Bookmark className="size-4" />}
          title="Reopen a saved test"
          body="Return to an earlier run with its assumptions intact. Completed runs are already stored — this is the front door to them."
        />
      </section>
    </div>
  )
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

  return (
    <article className="flex flex-col gap-3 bg-card p-5">
      <div className="flex items-start justify-between gap-2">
        <Badge variant="secondary">{template.family}</Badge>
        <span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
          {template.expectedFrequency} frequency
        </span>
      </div>

      <h3 className="text-base font-semibold leading-snug">{template.name}</h3>
      <p className="text-sm leading-relaxed text-muted-foreground">
        {template.description}
      </p>

      <p className="border-l-2 border-border pl-3 text-sm leading-relaxed">
        {template.described.summary}
      </p>

      <p className="mt-auto pt-1 font-mono text-[11px] text-muted-foreground">
        suggested: {displaySymbol(symbol)} · {displayTimeframe(timeframe)}
      </p>

      <Button className="w-full" onClick={() => onChoose(template)}>
        Customise and test
        <ArrowRight className="size-4" />
      </Button>
    </article>
  )
}

function PlannedTile({
  icon,
  title,
  body,
}: {
  icon: React.ReactNode
  title: string
  body: string
}) {
  return (
    <article className="border border-dashed border-border p-5">
      <div className="flex items-center gap-2 text-muted-foreground">
        {icon}
        <span className="font-mono text-[10px] uppercase tracking-wider">
          Not built yet
        </span>
      </div>
      <h3 className="mt-2 text-base font-semibold">{title}</h3>
      <p className="mt-1 text-sm leading-relaxed text-muted-foreground">
        {body}
      </p>
    </article>
  )
}

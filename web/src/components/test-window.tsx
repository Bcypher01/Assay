import { Link } from '@tanstack/react-router'
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
import { formatMs } from '@/lib/codec'
import { displaySymbol, displayTimeframe } from '@/lib/strategy'
import { hasReadyData, session, useSession } from '@/lib/store'

/**
 * The windows a person actually asks for. Hand-picking two dates to get "the
 * last three years" is busywork. Presets end at today's UTC midnight and run
 * backwards, matching how the engine reads the half-open window.
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

/**
 * Choosing the period belongs next to running the test, not one screen away.
 * This is the part of the old "Prepare the history" step that a person needs
 * to touch; preparation itself is an action on the ready check below.
 */
export function TestWindow() {
  const s = useSession()
  const ready = hasReadyData(s)
  const missing =
    s.dataStatus?.readiness === 'missing' ||
    s.dataStatus?.readiness === 'partial'

  return (
    <Card id="test-window">
      <CardHeader className="pb-3">
        <CardTitle>Test window</CardTitle>
        <CardDescription>
          Dates are read as UTC and the end date is not included. The test runs
          on the whole window you choose here.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-muted/30 p-3">
          <p className="text-sm">
            <span className="runlog-label">Testing</span>{' '}
            <span className="font-medium">
              {s.symbol && s.timeframe
                ? `${displaySymbol(s.symbol)} · ${displayTimeframe(s.timeframe)}`
                : 'No market selected'}
            </span>
          </p>
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
                matchesPreset(s.fromMs, s.toMs, years) ? 'default' : 'outline'
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
            <Label htmlFor="test-from">From (UTC)</Label>
            <DateField
              id="test-from"
              value={s.fromMs ?? null}
              onChange={(fromMs) => session.set({ fromMs })}
              placeholder="Start of window"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="test-to">To (UTC, exclusive)</Label>
            <DateField
              id="test-to"
              value={s.toMs ?? null}
              onChange={(toMs) => session.set({ toMs })}
              placeholder="End of window"
            />
          </div>
        </div>

        <p className="text-xs text-muted-foreground">
          {ready
            ? `Ready locally through ${formatMs(s.dataStatus?.cachedToMs ?? undefined)}.`
            : missing
              ? 'This window still needs preparing. Do that under the ready check below.'
              : s.fromMs !== null && s.toMs !== null
                ? 'Not checked yet. The ready check below verifies this exact window.'
                : 'Choose both dates, then check the local data below.'}
        </p>
      </CardContent>
    </Card>
  )
}

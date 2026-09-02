import { useState } from 'react'
import { Info, ChevronDown } from 'lucide-react'
import { CONSTRAINTS } from '@/lib/constraints'
import { cn } from '@/lib/utils'

/**
 * A compact, dismissible summary of the execution constraints. Rendered on the
 * screens where they bite (data prep, run) so limits are visible up front.
 */
export function ConstraintsBanner({ className }: { className?: string }) {
  const [open, setOpen] = useState(true)

  return (
    <div
      className={cn(
        'rounded-lg border border-border bg-muted/40 text-sm',
        className,
      )}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-4 py-3 text-left font-medium"
        aria-expanded={open}
      >
        <Info className="size-4 text-muted-foreground" />
        Execution constraints
        <ChevronDown
          className={cn(
            'ml-auto size-4 text-muted-foreground transition-transform',
            open && 'rotate-180',
          )}
        />
      </button>
      {open ? (
        <div className="grid gap-3 px-4 pb-4 sm:grid-cols-2">
          {CONSTRAINTS.map((c) => (
            <div key={c.title} className="space-y-0.5">
              <p className="font-medium text-foreground">{c.title}</p>
              <p className="text-xs leading-relaxed text-muted-foreground">
                {c.detail}
              </p>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  )
}

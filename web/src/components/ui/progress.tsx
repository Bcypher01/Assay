import * as React from 'react'
import { cn } from '@/lib/utils'

export interface ProgressProps extends React.HTMLAttributes<HTMLDivElement> {
  /** 0–100. Values outside the range are clamped. */
  value?: number
  /** Render an indeterminate shimmer instead of a fixed width. */
  indeterminate?: boolean
}

const Progress = React.forwardRef<HTMLDivElement, ProgressProps>(
  ({ className, value = 0, indeterminate = false, ...props }, ref) => {
    const pct = Math.max(0, Math.min(100, value))
    return (
      <div
        ref={ref}
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={indeterminate ? undefined : Math.round(pct)}
        className={cn(
          'relative h-2 w-full overflow-hidden rounded-full bg-secondary',
          className,
        )}
        {...props}
      >
        <div
          className={cn(
            'h-full rounded-full bg-primary transition-[width] duration-300 ease-out',
            indeterminate && 'progress-indeterminate w-1/3',
          )}
          style={indeterminate ? undefined : { width: `${pct}%` }}
        />
      </div>
    )
  },
)
Progress.displayName = 'Progress'

export { Progress }

import * as React from 'react'
import { cn } from '@/lib/utils'

/**
 * A placeholder block that pulses while real content loads. It inherits the
 * app's square corners inside the stage, so a skeleton reads as the same
 * surface the content will occupy — not a rounded chip laid over it.
 */
function Skeleton({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        'animate-pulse bg-muted motion-reduce:animate-none',
        className,
      )}
      aria-hidden="true"
      {...props}
    />
  )
}

export { Skeleton }

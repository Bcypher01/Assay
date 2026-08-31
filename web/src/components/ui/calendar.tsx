import * as React from 'react'
import { ChevronDown, ChevronLeft, ChevronRight } from 'lucide-react'
import { DayPicker } from 'react-day-picker'
import { cn } from '@/lib/utils'
import { buttonVariants } from '@/components/ui/button'

export type CalendarProps = React.ComponentProps<typeof DayPicker>

/**
 * shadcn's Calendar, on react-day-picker v9.
 *
 * The caption is a month + year dropdown pair rather than arrows alone, so a
 * window years away is one click, not thirty. The nav band spans the caption
 * with the arrows pinned to its edges and the caption padded clear of them —
 * the arrows never sit under the label, which is what made them miss clicks.
 */
function Calendar({
  className,
  classNames,
  showOutsideDays = true,
  captionLayout = 'dropdown',
  startMonth = new Date(1990, 0),
  endMonth = new Date(new Date().getFullYear() + 1, 11),
  ...props
}: CalendarProps) {
  return (
    <DayPicker
      showOutsideDays={showOutsideDays}
      captionLayout={captionLayout}
      startMonth={startMonth}
      endMonth={endMonth}
      className={cn('p-0', className)}
      classNames={{
        months: 'relative flex flex-col sm:flex-row gap-4',
        month: 'flex flex-col gap-4',
        // px-8 keeps the caption clear of the arrows pinned to each edge.
        month_caption: 'relative flex h-7 items-center justify-center px-8',
        caption_label: 'inline-flex items-center gap-1 text-sm font-medium',
        nav: 'absolute inset-x-0 top-0 flex h-7 items-center justify-between',
        button_previous: cn(
          buttonVariants({ variant: 'outline' }),
          'z-10 size-7 p-0 opacity-70 hover:opacity-100',
        ),
        button_next: cn(
          buttonVariants({ variant: 'outline' }),
          'z-10 size-7 p-0 opacity-70 hover:opacity-100',
        ),
        // Month + year selects. The native <select> is a transparent overlay
        // (z-20) over the visible label, so it is the reliable click target.
        dropdowns: 'flex items-center justify-center gap-1.5',
        dropdown_root:
          'relative inline-flex h-7 items-center rounded-md border border-input px-2 hover:bg-accent',
        dropdown: 'absolute inset-0 z-20 w-full cursor-pointer opacity-0',
        month_grid: 'w-full border-collapse space-y-1',
        weekdays: 'flex',
        weekday:
          'w-8 rounded-md text-[0.72rem] font-normal text-muted-foreground',
        week: 'mt-2 flex w-full',
        day: 'relative size-8 p-0 text-center text-sm focus-within:relative focus-within:z-20',
        day_button: cn(
          buttonVariants({ variant: 'ghost' }),
          'size-8 p-0 font-normal aria-selected:opacity-100',
        ),
        selected:
          '[&>button]:bg-primary [&>button]:text-primary-foreground [&>button]:hover:bg-primary [&>button]:hover:text-primary-foreground',
        today: '[&>button]:border [&>button]:border-input',
        outside: 'text-muted-foreground opacity-50',
        disabled: 'text-muted-foreground opacity-40',
        hidden: 'invisible',
        ...classNames,
      }}
      components={{
        Chevron: ({ orientation, ...chevronProps }) => {
          const Icon =
            orientation === 'left'
              ? ChevronLeft
              : orientation === 'right'
                ? ChevronRight
                : ChevronDown
          return <Icon className="size-4" {...chevronProps} />
        },
      }}
      {...props}
    />
  )
}
Calendar.displayName = 'Calendar'

export { Calendar }

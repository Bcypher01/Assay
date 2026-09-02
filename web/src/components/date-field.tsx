import * as React from 'react'
import { CalendarIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'

interface DateFieldProps {
  id?: string
  /** Epoch-ms at UTC midnight, or null when unset. */
  value: bigint | null
  onChange: (value: bigint | null) => void
  placeholder?: string
  disabled?: boolean
  className?: string
}

const display = new Intl.DateTimeFormat('en-GB', {
  timeZone: 'UTC',
  day: '2-digit',
  month: 'short',
  year: 'numeric',
})

/**
 * Dates in this app are UTC midnights, but react-day-picker reasons in local
 * time. These two convert between the two worlds by calendar parts rather than
 * by offset, which is the only version that cannot drift by a day.
 */
function msToPickerDate(ms: bigint | null): Date | undefined {
  if (ms === null) return undefined
  const utc = new Date(Number(ms))
  if (Number.isNaN(utc.getTime())) return undefined
  return new Date(utc.getUTCFullYear(), utc.getUTCMonth(), utc.getUTCDate(), 12)
}

function pickerDateToMs(date: Date | undefined): bigint | null {
  if (!date) return null
  return BigInt(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()))
}

/**
 * A date field where the whole control is the trigger.
 *
 * The native `<input type="date">` only opens its calendar from the icon, and
 * puts that icon wherever the browser likes. Here the field is one button: a
 * click anywhere in it opens the calendar, and the icon is pinned to the far
 * right edge.
 */
export function DateField({
  id,
  value,
  onChange,
  placeholder = 'Pick a date',
  disabled,
  className,
}: DateFieldProps) {
  const [open, setOpen] = React.useState(false)
  const selected = msToPickerDate(value)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          id={id}
          type="button"
          variant="outline"
          disabled={disabled}
          className={cn(
            'h-9 w-full justify-between gap-2 rounded-md pl-3 pr-2 text-sm font-normal shadow-sm',
            !selected && 'text-muted-foreground',
            className,
          )}
        >
          <span className="truncate">
            {value !== null
              ? display.format(new Date(Number(value)))
              : placeholder}
          </span>
          <CalendarIcon className="opacity-60" aria-hidden="true" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="p-3">
        <Calendar
          mode="single"
          autoFocus
          selected={selected}
          defaultMonth={selected}
          onSelect={(date) => {
            onChange(pickerDateToMs(date))
            setOpen(false)
          }}
        />
      </PopoverContent>
    </Popover>
  )
}

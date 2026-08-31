import { useMemo } from 'react'

/**
 * A dependency-free equity curve. Values arrive as fixed-point int64 (bigint);
 * we plot them relative to their own min/max, so the shape is faithful without
 * implying an absolute scale.
 */
export function EquitySparkline({
  values,
  height = 160,
}: {
  values: bigint[]
  height?: number
}) {
  const path = useMemo(() => {
    if (values.length < 2) return null
    const nums = values.map((v) => Number(v))
    let min = nums[0]
    let max = nums[0]
    for (const n of nums) {
      if (n < min) min = n
      if (n > max) max = n
    }
    const span = max - min || 1
    const w = 1000
    const h = 1000
    const step = w / (nums.length - 1)
    const pts = nums.map((n, i) => {
      const x = i * step
      const y = h - ((n - min) / span) * h
      return `${x.toFixed(2)},${y.toFixed(2)}`
    })
    return {
      line: `M${pts.join(' L')}`,
      area: `M0,${h} L${pts.join(' L')} L${w},${h} Z`,
      last: nums[nums.length - 1],
      first: nums[0],
    }
  }, [values])

  if (!path) {
    return (
      <div
        className="flex items-center justify-center rounded-md border border-dashed border-border text-xs text-muted-foreground"
        style={{ height }}
      >
        Waiting for equity data…
      </div>
    )
  }

  const up = path.last >= path.first
  const stroke = up ? 'hsl(var(--success))' : 'hsl(var(--destructive))'

  return (
    <svg
      viewBox="0 0 1000 1000"
      preserveAspectRatio="none"
      className="w-full rounded-md border border-border bg-muted/20"
      style={{ height }}
      role="img"
      aria-label="Equity curve"
    >
      <path d={path.area} fill={stroke} opacity={0.12} />
      <path
        d={path.line}
        fill="none"
        stroke={stroke}
        strokeWidth={6}
        vectorEffect="non-scaling-stroke"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  )
}

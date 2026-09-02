export type ChartTool =
  'select' | 'level' | 'trend' | 'zone' | 'fib' | 'long' | 'short' | 'note'

export interface ChartPoint {
  timeMs: number
  price: number
}

export interface ChartDrawing {
  id: string
  type: Exclude<ChartTool, 'select'>
  from: ChartPoint
  to: ChartPoint
  text?: string
}

export function drawingID(): string {
  return crypto.randomUUID()
}

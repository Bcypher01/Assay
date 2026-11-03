import type { ReactNode } from 'react'

export function PageHeader({
  title,
  description,
  actions,
  step,
  totalSteps = 5,
}: {
  title: string
  description?: ReactNode
  actions?: ReactNode
  step?: number
  totalSteps?: number
}) {
  return (
    <div className="runlog-page-header">
      <div className="space-y-1">
        <p className="runlog-label">
          {step ? `Step ${step} of ${totalSteps}` : 'Assay study'}
        </p>
        <h1>{title}</h1>
        {description ? (
          <p className="max-w-2xl text-sm text-muted-foreground">
            {description}
          </p>
        ) : null}
      </div>
      {actions ? <div className="runlog-page-actions">{actions}</div> : null}
    </div>
  )
}

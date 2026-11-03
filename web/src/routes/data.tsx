import { createFileRoute, redirect } from '@tanstack/react-router'

/**
 * Preparing history is part of the test step now: the date window and the
 * prepare action live together, so a person never has to visit a separate
 * screen to make a run possible. The old route stays as a redirect because
 * links to it exist in older notes, bookmarks and logs.
 */
export const Route = createFileRoute('/data')({
  beforeLoad: () => {
    throw redirect({ to: '/run' })
  },
})

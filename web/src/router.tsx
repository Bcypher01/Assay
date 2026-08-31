import {
  createRouter as createTanStackRouter,
  Link,
} from '@tanstack/react-router'
import { routeTree } from './routeTree.gen'

function NotFound() {
  return (
    <div className="mx-auto max-w-md space-y-3 px-6 py-16 text-center">
      <p className="text-sm font-medium uppercase tracking-wide text-muted-foreground">
        404
      </p>
      <h1 className="text-2xl font-semibold tracking-tight">Page not found</h1>
      <p className="text-sm text-muted-foreground">
        That route doesn’t exist. Head back to the overview to pick a step.
      </p>
      <Link
        to="/"
        className="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90"
      >
        Back to overview
      </Link>
    </div>
  )
}

export function getRouter() {
  const router = createTanStackRouter({
    routeTree,
    scrollRestoration: true,
    defaultPreload: 'intent',
    defaultPreloadStaleTime: 0,
    defaultNotFoundComponent: NotFound,
  })

  return router
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof getRouter>
  }

  /**
   * Per-route flags the root layout reads.
   *
   * `fullscreen` opts a route out of the run-log shell entirely — no spine, no
   * page column. It is for the screens that are workspaces rather than steps,
   * where the surface itself is the whole interface and a persistent nav
   * beside it only takes pixels away from the thing being worked on.
   */
  interface StaticDataRouteOption {
    fullscreen?: boolean
  }
}

import {
  HeadContent,
  Outlet,
  Scripts,
  createRootRoute,
  useMatches,
} from '@tanstack/react-router'
import { TanStackRouterDevtoolsPanel } from '@tanstack/react-router-devtools'
import { TanStackDevtools } from '@tanstack/react-devtools'
import { AlertTriangle, Home, RefreshCw } from 'lucide-react'
import { AppNav } from '@/components/app-nav'
import { Button } from '@/components/ui/button'

import appCss from '../styles.css?url'

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'Assay — Honest Backtester' },
    ],
    links: [
      { rel: 'stylesheet', href: appCss },
      { rel: 'icon', href: '/favicon.svg', type: 'image/svg+xml' },
    ],
  }),
  shellComponent: RootDocument,
  component: RootLayout,
  errorComponent: RootError,
})

function RootDocument({ children }: { children: React.ReactNode }) {
  // Developer tooling should never sit over the product by default—especially
  // on phones, where its floating launcher can cover an entire form field.
  // Opt in locally with VITE_ENABLE_DEVTOOLS=true when actively debugging.
  const showDevtools =
    import.meta.env.DEV && import.meta.env.VITE_ENABLE_DEVTOOLS === 'true'

  return (
    <html lang="en">
      <head>
        <HeadContent />
        {/* Apply a saved appearance before first paint so a manual theme never
            flashes the system default on the way in. This runs identically on
            the server and the client, so it cannot cause a hydration mismatch. */}
        <script
          dangerouslySetInnerHTML={{
            __html:
              "(function(){try{var t=localStorage.getItem('assay.theme');if(t==='light'||t==='dark'){document.documentElement.setAttribute('data-theme',t);}}catch(e){}})();",
          }}
        />
      </head>
      <body>
        {children}
        {showDevtools ? (
          <TanStackDevtools
            config={{ position: 'bottom-right' }}
            plugins={[
              {
                name: 'Tanstack Router',
                render: <TanStackRouterDevtoolsPanel />,
              },
            ]}
          />
        ) : null}
        <Scripts />
      </body>
    </html>
  )
}

function RootLayout() {
  // A route can opt out of the shell entirely (see StaticDataRouteOption in
  // router.tsx). The run-log spine is the wayfinder for the seven-step study
  // flow; on a workspace screen like the setup chart it is a permanent 276px
  // tax on the surface the user is actually working on, and the step it would
  // highlight is not the one they are in.
  const matches = useMatches()
  if (matches.some((match) => match.staticData.fullscreen)) {
    return <Outlet />
  }

  return (
    <div className="assay-shell">
      <AppNav />
      <main className="assay-main">
        <div className="assay-stage">
          <Outlet />
        </div>
      </main>
    </div>
  )
}

function RootError({ error }: { error: unknown }) {
  const message =
    error instanceof Error
      ? error.message
      : typeof error === 'string'
        ? error
        : 'The current view failed to render.'

  return (
    <div className="assay-shell">
      <AppNav />
      <main className="assay-main">
        <div className="mx-auto flex min-h-[70dvh] w-full max-w-3xl items-center">
          <section className="w-full rounded-lg border border-destructive/35 bg-background p-6 shadow-sm">
            <div className="flex items-start gap-4">
              <div className="rounded-md bg-destructive/10 p-2 text-destructive">
                <AlertTriangle className="size-5" />
              </div>
              <div className="min-w-0 flex-1 space-y-4">
                <div>
                  <h1 className="text-xl font-semibold tracking-normal">
                    This view needs a refresh
                  </h1>
                  <p className="mt-2 text-sm text-muted-foreground">
                    The app caught a rendering error before it could interrupt
                    the rest of the workflow.
                  </p>
                </div>
                <pre className="max-h-32 overflow-auto rounded-md bg-muted p-3 text-xs text-muted-foreground">
                  {message}
                </pre>
                <div className="flex flex-wrap gap-2">
                  <Button onClick={() => window.location.reload()}>
                    <RefreshCw className="size-4" />
                    Refresh
                  </Button>
                  <Button variant="outline" asChild>
                    <a href="/data">
                      <Home className="size-4" />
                      Go to data
                    </a>
                  </Button>
                </div>
              </div>
            </div>
          </section>
        </div>
      </main>
    </div>
  )
}

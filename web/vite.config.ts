import { defineConfig } from 'vite'
import { devtools } from '@tanstack/devtools-vite'

import { tanstackStart } from '@tanstack/react-start/plugin/vite'

import viteReact from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// The UI runs on :3000; the local Assay server listens on :8080. Proxying
// "/api" avoids cross-origin issues while keeping streaming intact. The RPC
// client's default baseUrl is "/api" (see src/lib/rpc.ts); override with
// VITE_ASSAY_BASE_URL to talk to the server directly instead.
const ASSAY_SERVER_URL = process.env.ASSAY_SERVER_URL ?? 'http://127.0.0.1:8080'

const config = defineConfig({
  resolve: { tsconfigPaths: true },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: ASSAY_SERVER_URL,
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, ''),
      },
    },
  },
  plugins: [
    // Source injection rewrites data-tsd-source attributes at runtime with
    // __tsd/open-source URLs, so the client markup no longer matches the SSR
    // HTML. That is a hydration mismatch on every dev page load, and console
    // piping then amplifies each one into a recursive flood. The devtools
    // panel itself still works with both off.
    devtools({
      injectSource: { enabled: false },
      consolePiping: { enabled: false },
    }),
    tailwindcss(),
    tanstackStart(),
    viteReact(),
  ],
})

export default config

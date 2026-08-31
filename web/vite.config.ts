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
  plugins: [devtools(), tailwindcss(), tanstackStart(), viteReact()],
})

export default config

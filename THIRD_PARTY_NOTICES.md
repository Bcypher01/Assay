# Third-party notices

Assay is distributed under the Apache License 2.0. It includes the
following third-party software, which remains under its own licence.

## Go dependencies

| Module | Licence | Source |
|---|---|---|
| `connectrpc.com/connect` | Apache-2.0 | https://github.com/connectrpc/connect-go/blob/v1.18.1/LICENSE |
| `github.com/andybalholm/brotli` | MIT | https://github.com/andybalholm/brotli/blob/v1.1.0/LICENSE |
| `github.com/dustin/go-humanize` | MIT | https://github.com/dustin/go-humanize/blob/v1.0.1/LICENSE |
| `github.com/google/uuid` | BSD-3-Clause | https://github.com/google/uuid/blob/v1.6.0/LICENSE |
| `github.com/klauspost/compress` | Apache-2.0 | https://github.com/klauspost/compress/blob/v1.17.9/LICENSE |
| `github.com/klauspost/compress/internal/snapref` | BSD-3-Clause | https://github.com/klauspost/compress/blob/v1.17.9/internal/snapref/LICENSE |
| `github.com/klauspost/compress/s2` | BSD-3-Clause | https://github.com/klauspost/compress/blob/v1.17.9/s2/LICENSE |
| `github.com/klauspost/compress/snappy` | BSD-3-Clause | https://github.com/klauspost/compress/blob/v1.17.9/snappy/LICENSE |
| `github.com/klauspost/compress/zstd/internal/xxhash` | MIT | https://github.com/klauspost/compress/blob/v1.17.9/zstd/internal/xxhash/LICENSE.txt |
| `github.com/mattn/go-isatty` | MIT | https://github.com/mattn/go-isatty/blob/v0.0.20/LICENSE |
| `github.com/ncruces/go-strftime` | MIT | https://github.com/ncruces/go-strftime/blob/v0.1.9/LICENSE |
| `github.com/parquet-go/parquet-go` | Apache-2.0 | https://github.com/parquet-go/parquet-go/blob/v0.25.1/LICENSE |
| `github.com/parquet-go/parquet-go/bloom/xxhash` | MIT | https://github.com/parquet-go/parquet-go/blob/v0.25.1/bloom/xxhash/LICENSE |
| `github.com/parquet-go/parquet-go/encoding/thrift` | MIT | https://github.com/parquet-go/parquet-go/blob/v0.25.1/encoding/thrift/LICENSE |
| `github.com/pierrec/lz4/v4` | BSD-3-Clause | https://github.com/pierrec/lz4/blob/v4.1.21/LICENSE |
| `github.com/remyoudompheng/bigfft` | BSD-3-Clause | https://github.com/remyoudompheng/bigfft/blob/24d4a6f8daec/LICENSE |
| `github.com/ulikunitz/xz` | BSD-3-Clause | https://github.com/ulikunitz/xz/blob/v0.5.15/LICENSE |
| `golang.org/x/sys` | BSD-3-Clause | https://cs.opensource.google/go/x/sys/+/v0.22.0:LICENSE |
| `google.golang.org/protobuf` | BSD-3-Clause | https://github.com/protocolbuffers/protobuf-go/blob/v1.34.2/LICENSE |
| `modernc.org/libc` | BSD-3-Clause | https://gitlab.com/cznic/libc/blob/v1.55.3/LICENSE-GO |
| `modernc.org/libc/honnef.co/go/netdb` | MIT | https://gitlab.com/cznic/libc/blob/v1.55.3/honnef.co/go/netdb/LICENSE |
| `modernc.org/mathutil` | BSD-3-Clause | https://gitlab.com/cznic/mathutil/blob/v1.6.0/LICENSE |
| `modernc.org/memory` | BSD-3-Clause | https://gitlab.com/cznic/memory/blob/v1.8.0/LICENSE-GO |
| `modernc.org/sqlite` | BSD-3-Clause | https://gitlab.com/cznic/sqlite/blob/v1.34.5/LICENSE |

## Web production dependencies

The browser build and its Node server bundle the following production
dependencies. The full machine-readable list can be regenerated with
`bunx license-checker --production --csv --start web`.

### (Apache-2.0 AND BSD-3-Clause)

- `@bufbuild/protobuf@1.10.1`

### 0BSD

- `tslib@2.8.1`

### Apache-2.0

- `@connectrpc/connect-web@1.7.0`
- `@connectrpc/connect@1.7.0`
- `baseline-browser-mapping@2.11.18`
- `class-variance-authority@0.7.1`
- `detect-libc@2.1.2`

### BSD-3-Clause

- `diff@8.0.4`
- `source-map-js@1.2.1`
- `source-map@0.7.6`

### CC-BY-4.0

- `caniuse-lite@1.0.30001809`

### ISC

- `ansis@4.3.1`
- `electron-to-chromium@1.5.412`
- `graceful-fs@4.2.11`
- `lru-cache@5.1.1`
- `lucide-react@1.33.0`
- `picocolors@1.1.1`
- `semver@6.3.1`
- `yallist@3.1.1`

### MIT

- `@babel/code-frame@7.27.1`
- `@babel/code-frame@7.29.7`
- `@babel/compat-data@7.29.7`
- `@babel/core@7.29.7`
- `@babel/generator@7.29.8`
- `@babel/helper-compilation-targets@7.29.7`
- `@babel/helper-globals@7.29.7`
- `@babel/helper-module-imports@7.29.7`
- `@babel/helper-module-transforms@7.29.7`
- `@babel/helper-string-parser@7.29.7`
- `@babel/helper-validator-identifier@7.29.7`
- `@babel/helper-validator-option@7.29.7`
- `@babel/helpers@7.29.7`
- `@babel/parser@7.29.8`
- `@babel/template@7.29.7`
- `@babel/traverse@7.29.8`
- `@babel/types@7.29.8`
- `@date-fns/tz@1.5.0`
- `@floating-ui/core@1.8.0`
- `@floating-ui/dom@1.8.0`
- `@floating-ui/react-dom@2.1.9`
- `@floating-ui/utils@0.2.12`
- `@jridgewell/gen-mapping@0.3.13`
- `@jridgewell/remapping@2.3.5`
- `@jridgewell/resolve-uri@3.1.2`
- `@jridgewell/sourcemap-codec@1.5.5`
- `@jridgewell/trace-mapping@0.3.31`
- `@neodrag/core@3.0.0-next.11`
- `@neodrag/solid@3.0.0-next.11`
- `@oozcitak/dom@2.0.2`
- `@oozcitak/infra@2.0.2`
- `@oozcitak/url@3.0.0`
- `@oozcitak/util@10.0.0`
- `@oxc-project/types@0.146.0`
- `@radix-ui/number@1.1.3`
- `@radix-ui/primitive@1.1.7`
- `@radix-ui/react-arrow@1.1.15`
- `@radix-ui/react-collection@1.1.15`
- `@radix-ui/react-compose-refs@1.1.5`
- `@radix-ui/react-context@1.2.2`
- `@radix-ui/react-dialog@1.1.23`
- `@radix-ui/react-direction@1.1.4`
- `@radix-ui/react-dismissable-layer@1.1.19`
- `@radix-ui/react-focus-guards@1.1.6`
- `@radix-ui/react-focus-scope@1.1.16`
- `@radix-ui/react-id@1.1.4`
- `@radix-ui/react-label@2.1.15`
- `@radix-ui/react-popover@1.1.23`
- `@radix-ui/react-popper@1.3.7`
- `@radix-ui/react-portal@1.1.17`
- `@radix-ui/react-presence@1.1.10`
- `@radix-ui/react-primitive@2.1.10`
- `@radix-ui/react-roving-focus@1.1.19`
- `@radix-ui/react-select@2.3.7`
- `@radix-ui/react-slot@1.3.3`
- `@radix-ui/react-tabs@1.1.21`
- `@radix-ui/react-use-callback-ref@1.1.4`
- `@radix-ui/react-use-controllable-state@1.2.6`
- `@radix-ui/react-use-effect-event@0.0.5`
- `@radix-ui/react-use-is-hydrated@0.1.3`
- `@radix-ui/react-use-layout-effect@1.1.4`
- `@radix-ui/react-use-previous@1.1.4`
- `@radix-ui/react-use-rect@1.1.4`
- `@radix-ui/react-use-size@1.1.4`
- `@radix-ui/react-visually-hidden@1.2.11`
- `@radix-ui/rect@1.1.3`
- `@rolldown/binding-darwin-x64@1.2.5`
- `@rolldown/pluginutils@1.0.1`
- `@solid-primitives/event-listener@2.4.6`
- `@solid-primitives/keyboard@1.3.7`
- `@solid-primitives/resize-observer@2.2.0`
- `@solid-primitives/rootless@1.5.4`
- `@solid-primitives/static-store@0.1.4`
- `@solid-primitives/utils@6.4.1`
- `@tabby_ai/hijri-converter@1.0.5`
- `@tailwindcss/node@4.3.3`
- `@tailwindcss/oxide-darwin-x64@4.3.3`
- `@tailwindcss/oxide@4.3.3`
- `@tailwindcss/vite@4.3.3`
- `@tanstack/devtools-client@0.0.8`
- `@tanstack/devtools-event-bus@0.4.3`
- `@tanstack/devtools-event-client@0.5.0`
- `@tanstack/devtools-ui@0.7.1`
- `@tanstack/devtools@0.14.2`
- `@tanstack/history@1.162.1`
- `@tanstack/react-devtools@0.10.12`
- `@tanstack/react-router-devtools@1.167.1`
- `@tanstack/react-router@1.170.32`
- `@tanstack/react-start-client@1.168.30`
- `@tanstack/react-start-rsc@0.1.48`
- `@tanstack/react-start-server@1.167.37`
- `@tanstack/react-start@1.168.49`
- `@tanstack/react-store@0.9.3`
- `@tanstack/router-core@1.171.27`
- `@tanstack/router-devtools-core@1.168.1`
- `@tanstack/router-generator@1.167.33`
- `@tanstack/router-plugin@1.168.35`
- `@tanstack/router-utils@1.162.2`
- `@tanstack/start-client-core@1.170.27`
- `@tanstack/start-fn-stubs@1.162.0`
- `@tanstack/start-plugin-core@1.171.39`
- `@tanstack/start-server-core@1.169.31`
- `@tanstack/start-storage-context@1.167.29`
- `@tanstack/store@0.9.3`
- `@tanstack/virtual-file-routes@1.162.0`
- `@types/node@22.20.1`
- `@types/react-dom@19.2.5`
- `@types/react@19.2.18`
- `aria-hidden@1.2.6`
- `babel-dead-code-elimination@1.0.12`
- `browserslist@4.28.8`
- `chokidar@5.0.0`
- `clsx@2.1.1`
- `convert-source-map@2.0.0`
- `cookie-es@3.1.1`
- `csstype@3.2.3`
- `date-fns-jalali@4.1.0-0`
- `date-fns@4.4.0`
- `dayjs@1.11.23`
- `debug@4.4.3`
- `detect-node-es@1.1.0`
- `enhanced-resolve@5.24.5`
- `escalade@3.2.0`
- `exsolve@1.1.1`
- `fdir@6.5.0`
- `fetchdts@0.1.7`
- `fsevents@2.3.3`
- `gensync@1.0.0-beta.2`
- `get-nonce@1.0.1`
- `goober@2.1.19`
- `h3@2.0.1-rc.20`
- `jiti@2.7.0`
- `js-tokens@4.0.0`
- `js-yaml@4.3.1`
- `jsesc@3.1.0`
- `json5@2.2.3`
- `magic-string@0.30.21`
- `ms@2.1.3`
- `nanoid@3.3.18`
- `node-releases@2.0.53`
- `pathe@2.0.3`
- `picomatch@4.0.5`
- `postcss@8.5.26`
- `prettier@3.9.6`
- `react-day-picker@9.14.0`
- `react-dom@19.2.8`
- `react-remove-scroll-bar@2.3.8`
- `react-remove-scroll@2.7.2`
- `react-style-singleton@2.2.3`
- `react@19.2.8`
- `readdirp@5.1.1`
- `rolldown@1.2.5`
- `rou3@0.8.1`
- `scheduler@0.27.0`
- `seroval-plugins@1.5.6`
- `seroval-plugins@1.6.3`
- `seroval@1.5.6`
- `seroval@1.6.3`
- `solid-js@1.9.15`
- `srvx@0.11.22`
- `tailwind-merge@3.6.0`
- `tailwindcss@4.3.3`
- `tapable@2.3.3`
- `tinyglobby@0.2.17`
- `ufo@1.6.4`
- `undici-types@6.21.0`
- `unplugin@3.3.0`
- `update-browserslist-db@1.3.1`
- `use-callback-ref@1.3.3`
- `use-sidecar@1.1.3`
- `use-sync-external-store@1.6.0`
- `vite@8.2.2`
- `vitefu@1.1.3`
- `webpack-virtual-modules@0.6.2`
- `ws@8.21.3`
- `xmlbuilder2@4.0.3`
- `zod@4.4.3`

### MPL-2.0

- `lightningcss-darwin-x64@1.32.0`
- `lightningcss-darwin-x64@1.33.0`
- `lightningcss@1.32.0`
- `lightningcss@1.33.0`

### Python-2.0

- `argparse@2.0.1`

### Unlicense

- `isbot@5.2.1`

## Fonts

Instrument Sans, Spline Sans Mono and Syne are licensed under the SIL Open
Font License 1.1. Their licence texts are in `web/public/fonts/`.

## Data

Market data is not included in this repository and is not covered by the
Apache License 2.0. Data fetched through Assay remains subject to the terms
of the venue that supplied it.

//  @ts-check

import { tanstackConfig } from '@tanstack/eslint-config'

export default [
  ...tanstackConfig,
  {
    rules: {
      'import/no-cycle': 'off',
      'import/order': 'off',
      'sort-imports': 'off',
      '@typescript-eslint/array-type': 'off',
      '@typescript-eslint/require-await': 'off',
      // Types here describe the wire contract and the browser optimistically:
      // ConnectRPC messages arrive from a network boundary, and APIs such as
      // navigator.clipboard are genuinely absent in insecure contexts. Deleting
      // a runtime guard this rule calls redundant introduces a real crash.
      '@typescript-eslint/no-unnecessary-condition': 'off',
      'pnpm/json-enforce-catalog': 'off',
    },
  },
  {
    ignores: ['eslint.config.js', 'prettier.config.js'],
  },
]

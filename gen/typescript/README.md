# Assay TypeScript RPC client

This directory contains TypeScript artifacts generated from the checked-in
Assay protobuf definitions. It is deliberately a small standalone package so a
browser UI can consume the same service contract without copying types.

```sh
cd gen/typescript
npm install
npm test
```

`npm run generate` regenerates `src/` with the official Buf protobuf and
ConnectRPC generators. The generated service descriptors work with
`@connectrpc/connect`'s `createClient()` and a browser transport from
`@connectrpc/connect-web`.

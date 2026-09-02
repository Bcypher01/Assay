import { createServer } from 'node:http'
import { readFile, stat } from 'node:fs/promises'
import { extname, resolve, sep } from 'node:path'
import { Readable } from 'node:stream'

import app from './dist/server/server.js'

const host = process.env.HOST ?? '0.0.0.0'
const port = Number(process.env.PORT ?? 3000)
const clientDir = resolve('dist/client')

const contentTypes = {
  '.css': 'text/css; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.woff2': 'font/woff2',
}

function requestHeaders(headers) {
  return Object.fromEntries(
    Object.entries(headers)
      .filter(([, value]) => value !== undefined)
      .map(([key, value]) => [
        key,
        Array.isArray(value) ? value.join(', ') : value,
      ]),
  )
}

async function serveAsset(req, res, pathname) {
  if (req.method !== 'GET' && req.method !== 'HEAD') return false

  let decodedPath
  try {
    decodedPath = decodeURIComponent(pathname)
  } catch {
    return false
  }
  const file = resolve(clientDir, `.${decodedPath}`)
  if (file !== clientDir && !file.startsWith(`${clientDir}${sep}`)) return false

  try {
    if (!(await stat(file)).isFile()) return false
  } catch {
    return false
  }

  res.statusCode = 200
  res.setHeader(
    'Content-Type',
    contentTypes[extname(file)] ?? 'application/octet-stream',
  )
  if (pathname.startsWith('/assets/')) {
    res.setHeader('Cache-Control', 'public, max-age=31536000, immutable')
  }
  if (req.method === 'HEAD') {
    res.end()
    return true
  }
  res.end(await readFile(file))
  return true
}

createServer(async (req, res) => {
  const origin = `http://${req.headers.host ?? `${host}:${port}`}`
  const url = new URL(req.url ?? '/', origin)

  try {
    if (await serveAsset(req, res, url.pathname)) return

    const method = req.method ?? 'GET'
    const request = new Request(url, {
      method,
      headers: requestHeaders(req.headers),
      body:
        method === 'GET' || method === 'HEAD' ? undefined : Readable.toWeb(req),
      duplex: method === 'GET' || method === 'HEAD' ? undefined : 'half',
    })
    const response = await app.fetch(request)
    res.statusCode = response.status
    response.headers.forEach((value, key) => res.setHeader(key, value))
    if (!response.body) {
      res.end()
      return
    }
    Readable.fromWeb(response.body).pipe(res)
  } catch (error) {
    console.error('web request failed', error)
    res.statusCode = 500
    res.setHeader('Content-Type', 'text/plain; charset=utf-8')
    res.end('Internal Server Error')
  }
}).listen(port, host, () => {
  console.log(`Assay UI listening on http://${host}:${port}`)
})

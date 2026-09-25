// Does the Open Wall paint for a reader who arrives through a mirror?
//
//   # what a mirror does today
//   docker run --rm --network host -v "$PWD/tools":/w -w /w \
//     mcr.microsoft.com/playwright:v1.49.1-noble \
//     sh -c 'npm i -s playwright@1.49.1 >/dev/null; node mirror-check.mjs openipc.kz'
//
//   # what it does with a bundle built here, served under that same name
//   ... node mirror-check.mjs openipc.kz --dist /w/dist
//
// WHY A SIMULATED MIRROR. openipc.ru, openipc.kz and openipc.cloud are other
// people's hosts; this project has a shell on one of the three. The fault they
// produce is not exotic -- nginx proxies to the origin over HTTP/1.0 unless
// told otherwise, HTTP/1.0 cannot carry an `Upgrade`, so the socket handshake
// arrives at the origin as an ordinary GET and is answered 404 -- and it can
// be reproduced exactly by serving the bundle over TLS under the mirror's own
// name and refusing the upgrade. What that buys is the thing the real mirror
// cannot give: the page's origin is `https://openipc.kz`, which is what the
// origin's `allowed_request_origins` is checked against, so the fallback is
// exercised for real rather than described.
//
// `--host-resolver-rules` is what puts the browser on that name without
// touching /etc/hosts or DNS, and the certificate is self-signed for the same
// reason, so the context ignores TLS errors. Nothing here reaches a mirror.
//
// Painted is decided by reading pixels back, as tools/canvas-check.mjs does:
// every other signal is something the page sets and could set while painting
// nothing.
import { execFileSync } from 'node:child_process'
import { createServer } from 'node:https'
import { mkdtempSync, readFileSync, statSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, normalize } from 'node:path'
import { chromium } from 'playwright'

const args = process.argv.slice(2)
const host = args[0]
const opt = (name, fallback) => {
  const i = args.indexOf(`--${name}`)
  return i === -1 ? fallback : args[i + 1]
}
if (!host || host.startsWith('--')) {
  console.error('usage: node mirror-check.mjs <host> [--dist <dir>] [--path /ru] [--origin <url>] [--port 8443]')
  process.exit(2)
}

const dist = opt('dist', null)
const shot = opt('shot', null)
const path = opt('path', '/ru')
const origin = opt('origin', 'https://openipc.org')
const port = Number(opt('port', 8443))

let failed = 0
const note = (ok, msg) => { if (!ok) failed++; console.log(`${ok ? 'PASS' : 'FAIL'}  ${msg}`) }

const TYPES = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css',
  '.json': 'application/json', '.svg': 'image/svg+xml', '.webp': 'image/webp',
  '.png': 'image/png', '.jpg': 'image/jpeg', '.woff2': 'font/woff2', '.ico': 'image/x-icon',
}

let server
if (dist) {
  const dir = mkdtempSync(join(tmpdir(), 'mirror-'))
  execFileSync('openssl', [
    'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
    '-subj', `/CN=${host}`, '-keyout', join(dir, 'key.pem'), '-out', join(dir, 'cert.pem'),
  ], { stdio: 'ignore' })

  server = createServer({
    key: readFileSync(join(dir, 'key.pem')),
    cert: readFileSync(join(dir, 'cert.pem')),
  }, async (req, res) => {
    const url = new URL(req.url, `https://${host}`)

    // Everything the bundle cannot answer goes to the origin, which is what
    // the mirrors' `location /` does.
    if (url.pathname.startsWith('/api/')) {
      const upstream = await fetch(`${origin}${url.pathname}${url.search}`, {
        headers: { accept: req.headers.accept ?? '*/*' },
      })
      res.writeHead(upstream.status, { 'content-type': upstream.headers.get('content-type') ?? 'text/plain' })
      res.end(Buffer.from(await upstream.arrayBuffer()))
      return
    }

    // try_files $uri $uri/index.html, as the origin's own vhost does.
    const rel = normalize(decodeURIComponent(url.pathname)).replace(/^(\.\.[/\\])+/, '')
    for (const candidate of [join(dist, rel), join(dist, rel, 'index.html')]) {
      try {
        if (!statSync(candidate).isFile()) continue
      } catch { continue }
      const ext = candidate.slice(candidate.lastIndexOf('.'))
      res.writeHead(200, { 'content-type': TYPES[ext] ?? 'application/octet-stream' })
      res.end(readFileSync(candidate))
      return
    }
    res.writeHead(404, { 'content-type': 'text/plain' }).end('not found\n')
  })

  // THE FAULT ITSELF. A proxy that does not forward the upgrade leaves the
  // origin answering an ordinary 404 to what was meant to be a handshake, and
  // nothing in any log distinguishes that from nobody having visited.
  server.on('upgrade', (_req, socket) => {
    socket.write('HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n')
    socket.destroy()
  })

  await new Promise((resolve) => server.listen(port, '127.0.0.1', resolve))
  console.log(`serving ${dist} as https://${host} (upgrades refused, /api/ proxied to ${origin})`)
}

const browser = await chromium.launch({
  args: dist ? [`--host-resolver-rules=MAP ${host} 127.0.0.1:${port}`] : [],
})
const ctx = await browser.newContext({
  viewport: { width: 1280, height: 1000 },
  deviceScaleFactor: 2,
  ignoreHTTPSErrors: Boolean(dist),
})
const page = await ctx.newPage()

const sockets = []
page.on('websocket', (ws) => {
  const record = { url: ws.url(), frames: 0, error: null }
  ws.on('framereceived', () => { record.frames += 1 })
  ws.on('socketerror', (e) => { record.error = String(e) })
  sockets.push(record)
})
const errors = []
page.on('pageerror', (e) => errors.push(e.message))

const painted = () => page.evaluate(() => Array.from(
  document.querySelectorAll('canvas[data-wall-frame], canvas[role="img"]'),
).filter((c) => {
  try {
    const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data
    for (let i = 3; i < d.length; i += 4000) if (d[i] !== 0) return true
  } catch { /* tainted or zero-sized */ }
  return false
}).length)

await page.goto(`https://${host}${path}`, { waitUntil: 'domcontentloaded' })
await page.waitForFunction(
  () => document.querySelectorAll('canvas[data-wall-frame], canvas[role="img"]').length > 0,
  null, { timeout: 20000 },
).catch(() => {})

const canvases = await page.locator('canvas[data-wall-frame], canvas[role="img"]').count()
// Long enough for the second attempt: FALLBACK_AFTER is 2.5s and the mosaic
// paints in a quarter of a second once a socket is up.
const deadline = Date.now() + 20000
let drawn = 0
while (Date.now() < deadline && drawn < canvases) {
  drawn = await painted()
  if (drawn >= canvases) break
  await page.waitForTimeout(500)
}

console.log(`\n${host}${path}`)
note(canvases > 0, `${canvases} camera tiles on the page`)
note(drawn > 0, `${drawn} of ${canvases} frames painted`)
for (const s of sockets) {
  console.log(`      socket ${s.url} — ${s.frames} messages${s.error ? `, ${s.error}` : ''}`)
}
if (errors.length) console.log(`      page errors: ${errors.join(' | ')}`)

if (shot) {
  // The mosaic is at the top of the home page, and a full-page shot of a
  // marketing page is mostly not the thing under test.
  await page.locator('canvas[data-wall-frame], canvas[role="img"]').first()
    .scrollIntoViewIfNeeded().catch(() => {})
  await page.screenshot({ path: shot })
  console.log(`      ${shot}`)
}

await browser.close()
if (server) server.close()
process.exit(failed ? 1 : 0)

// How much can a bot take if it reads the grant a page is given?
//
//   docker run --rm --network host -v "$PWD/tools":/w -w /w \
//     mcr.microsoft.com/playwright:v1.49.1-noble \
//     sh -c 'npm i -s playwright@1.49.1 >/dev/null; \
//            node adapted-harvest-check.mjs https://openipc.org'
//
// WHY THIS EXISTS. bare-socket-check.mjs measures the attack as it was found:
// a client that opens the socket and never holds a grant. That is refused.
// But "refused" is not "prevented", and the obvious next move for the fleet is
// cheap: fetch the wall's JSON like any page does, lift the grant from it,
// open the socket, and take everything that grant allows.
//
// It does NOT assert a pass. It reports the yield, because the number that
// matters is how many frames one fetch is worth -- that is the exposure, and
// it is what any further hardening has to reduce.
//
// Run it against production after a change to the wall. A yield that climbs
// means an address started handing out more per fetch.

import { chromium } from 'playwright'

const [base, user, pass] = process.argv.slice(2)
if (!base) {
  console.error('usage: node adapted-harvest-check.mjs <base-url> [user] [pass]')
  process.exit(2)
}

const browser = await chromium.launch()
const context = await browser.newContext(
  user ? { httpCredentials: { username: user, password: pass } } : {},
)
const page = await context.newPage()

// What one JSON address grants: the grant and the frames it names, per variant.
async function facts(path) {
  const res = await page.request.get(`${base}${path}`)
  if (!res.ok()) return { grant: null, frames: [] }
  const body = await res.json()
  const frames = []
  for (const tile of body.tiles || []) frames.push({ id: tile.id, variant: body.variant })
  if (body.snapshot) frames.push({ id: body.snapshot.id, variant: body.variant })
  for (const tile of body.strip || []) frames.push({ id: tile.id, variant: body.strip_variant })
  return { grant: body.grant || null, frames }
}

async function yieldOf(path) {
  const held = await facts(path)
  if (!held.grant) return { path, drawn: held.frames.length, took: 0, bytes: 0, note: 'no grant here' }

  // Everything the grant allows, asked for the way the page would.
  const got = await page.evaluate(async ({ grant, frames }) => {
    const byVariant = new Map()
    frames.forEach(({ id, variant }) => {
      if (!byVariant.has(variant)) byVariant.set(variant, [])
      byVariant.get(variant).push(id)
    })

    return await new Promise((resolve) => {
      const seen = { took: 0, bytes: 0 }
      const ws = new WebSocket(`wss://${location.host}/api/v1/wall/socket`)
      const stop = () => { try { ws.close() } catch { /* closed */ } resolve(seen) }

      ws.onmessage = (event) => {
        const msg = JSON.parse(event.data)
        if (msg.type === 'hello') {
          ws.send(JSON.stringify({ type: 'grant', grant }))
          byVariant.forEach((ids, variant) => {
            for (let i = 0; i < ids.length; i += 8) {
              ws.send(JSON.stringify({ type: 'request', variant, ids: ids.slice(i, i + 8) }))
            }
          })
          return
        }
        if (msg.type === 'frame') {
          seen.took++
          seen.bytes += (msg.frame.length || 0)
        }
      }
      ws.onerror = () => stop()
      setTimeout(stop, 20000)
    })
  }, held)

  return { path, drawn: held.frames.length, took: got.took, bytes: got.bytes }
}

// A blank same-origin document, so the sockets below are legal but the site's
// own client never runs and never spends the budget this is trying to measure.
await page.goto(`${base}/robots.txt`, { waitUntil: 'domcontentloaded' })

const first = (await facts('/api/v1/wall/page/1.json')).frames[0]?.id

const targets = ['/api/v1/wall/mosaic.json', '/api/v1/wall/page/1.json']
if (first) targets.push(`/api/v1/wall/snapshot/${first}.json`)

console.log('What one fetch is worth to a bot that reads the grant:\n')
let total = 0
for (const path of targets) {
  const r = await yieldOf(path)
  total += r.took
  const mb = (r.bytes / (1024 * 1024)).toFixed(1)
  console.log(`  ${path.padEnd(52)} drew ${String(r.drawn).padStart(3)}  took ${String(r.took).padStart(3)}  ~${mb} MB${r.note ? '  (' + r.note + ')' : ''}`)
}
console.log(`\n  one visit to all of them: ${total} frames`)

await browser.close()

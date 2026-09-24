// How much can a bot take if it reads the grant out of the page?
//
//   docker run --rm --network host -v "$PWD/tools":/w -w /w \
//     mcr.microsoft.com/playwright:v1.49.1-noble \
//     sh -c 'npm i -s playwright@1.49.1 >/dev/null; \
//            node adapted-harvest-check.mjs https://openipc.org'
//
// WHY THIS EXISTS. bare-socket-check.mjs measures the attack as it was found:
// a client that opens the socket and never loads a page. #272 refuses that,
// and as of this morning production refuses it in practice. But "refused
// today" is not "prevented", and the obvious next move for the fleet is
// cheap -- it already downloads about a thousand snapshot pages every two
// hours, and the grant sits in that markup.
//
// So this plays the adapted attacker: fetch one page like any crawler, lift
// the grant from the HTML, open the socket, and take everything that grant
// allows. It does NOT assert a pass. It reports the yield, because the number
// that matters is how many frames one page fetch is worth -- that is the
// exposure, and it is what any further hardening has to reduce.
//
// Run it against production after a change to the wall. A yield that climbs
// means a page started handing out more per fetch.

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

async function yieldOf(path) {
  await page.goto(`${base}${path}`, { waitUntil: 'networkidle' })

  const held = await page.evaluate(() => {
    const el = document.querySelector('[data-wall-grant]')
    const frames = Array.from(document.querySelectorAll('canvas[data-wall-frame]'))
      .map((c) => ({ id: c.dataset.wallFrame, variant: c.dataset.wallVariant || 'thumb' }))
    return { grant: el ? el.dataset.wallGrant : null, frames }
  })

  if (!held.grant) return { path, drawn: held.frames.length, took: 0, bytes: 0, note: 'no grant on this page' }

  // Everything the grant allows, asked for the way the page would.
  const got = await page.evaluate(async ({ grant, frames }) => {
    const byVariant = new Map()
    frames.forEach(({ id, variant }) => {
      if (!byVariant.has(variant)) byVariant.set(variant, [])
      byVariant.get(variant).push(id)
    })

    return await new Promise((resolve) => {
      const seen = { took: 0, bytes: 0 }
      const ws = new WebSocket(`wss://${location.host}/api/v1/wall/cable`)
      const identifier = JSON.stringify({ channel: 'WallChannel', grant })
      const stop = () => { try { ws.close() } catch { /* closed */ } resolve(seen) }

      ws.onmessage = (event) => {
        const msg = JSON.parse(event.data)
        if (msg.type === 'welcome') return ws.send(JSON.stringify({ command: 'subscribe', identifier }))
        if (msg.type === 'reject_subscription') return stop()
        if (msg.type === 'confirm_subscription') {
          byVariant.forEach((ids, variant) => {
            for (let i = 0; i < ids.length; i += 8) {
              ws.send(JSON.stringify({
                command: 'message',
                identifier,
                data: JSON.stringify({ action: 'request_frames', variant, ids: ids.slice(i, i + 8) }),
              }))
            }
          })
          return
        }
        if (msg.message && msg.message.frame) {
          seen.took++
          seen.bytes += (msg.message.frame.length || 0)
        }
      }
      ws.onerror = () => stop()
      setTimeout(stop, 20000)
    })
  }, held)

  return { path, drawn: held.frames.length, took: got.took, bytes: got.bytes }
}

await page.goto(`${base}/open-wall`, { waitUntil: 'networkidle' })
const first = await page.$eval('[data-wall-frame]', (el) => el.dataset.wallFrame).catch(() => null)

const targets = ['/open-wall']
if (first) targets.push(`/snapshots/${first}`, `/snapshots/${first}/oneday`)

console.log('What one page fetch is worth to a bot that reads the grant:\n')
let total = 0
for (const path of targets) {
  const r = await yieldOf(path)
  total += r.took
  const mb = (r.bytes / (1024 * 1024)).toFixed(1)
  console.log(`  ${path.padEnd(42)} drew ${String(r.drawn).padStart(3)}  took ${String(r.took).padStart(3)}  ~${mb} MB${r.note ? '  (' + r.note + ')' : ''}`)
}
console.log(`\n  one visit to all three: ${total} frames`)

await browser.close()

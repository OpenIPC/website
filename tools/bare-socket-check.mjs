// Can a client that never rendered a wall page take frames?
//
//   PW=$(ssh -p 35242 root@openipc.org 'cat /srv/www/.dev-basic-auth-password')
//   docker run --rm --network host -v "$PWD/tools":/w -w /w \
//     mcr.microsoft.com/playwright:v1.49.1-noble \
//     sh -c 'npm i -s playwright@1.49.1 >/dev/null; \
//            node bare-socket-check.mjs https://dev.openipc.org openipc '"$PW"
//
// WHY THIS EXISTS. #267 moved frames onto WallChannel so that a crawler
// collecting URLs would find nothing to collect, and no address returns a
// frame any more. Measured in the first four hours after that shipped, 991 of
// the 1,055 addresses on the channel had requested NOTHING else -- no page, no
// asset, no favicon, only `GET /api/v1/wall/cable`. They took 2,393 MiB of the
// 2,437. They were not driving browsers; they were speaking the ActionCable
// protocol directly, which needs no page at all.
//
// That is the client this simulates. It loads a page with no frames on it --
// so the browser holds no grant -- then opens the socket by hand and asks for
// frames by id, exactly as the fleet does. The ids are real ones scraped from
// the gallery, because an id is public and guessing was never the hard part.
//
// The Ruby suite cannot show this: `subscribe` in a channel test is a method
// call, not a socket, and it proves nothing about what an outsider can reach
// through nginx. This runs the real protocol against the real deployment.

import { chromium } from 'playwright'

const [base, user, pass] = process.argv.slice(2)
if (!base) {
  console.error('usage: node bare-socket-check.mjs <base-url> [user] [pass]')
  process.exit(2)
}

let failures = 0
const ok = (m) => console.log(`PASS  ${m}`)
const bad = (m) => { failures++; console.log(`FAIL  ${m}`) }

const browser = await chromium.launch()
const context = await browser.newContext(
  user ? { httpCredentials: { username: user, password: pass } } : {},
)
const page = await context.newPage()

// Real ids, taken from the gallery the way anyone can.
await page.goto(`${base}/open-wall`, { waitUntil: 'networkidle' })
const ids = await page.$$eval('[data-wall-frame]', (els) =>
  els.map((e) => e.dataset.wallFrame).slice(0, 8))

if (ids.length === 0) {
  console.error('no frames on the gallery -- seed a camera day on dev first')
  process.exit(2)
}
ok(`scraped ${ids.length} real frame ids from the gallery markup`)

// A page with no frames, so nothing on it carries a grant.
await page.goto(`${base}/donate`, { waitUntil: 'domcontentloaded' })
const grantHere = await page.$('[data-wall-grant]')
if (grantHere) bad('the no-frames page carried a grant; this probe proves nothing')
else ok('the page this socket is opened from holds no grant')

// Speak ActionCable by hand, as the fleet does.
const result = await page.evaluate(async ({ ids }) => {
  const seen = { frames: 0, rejected: false, errors: [], confirmed: false }

  return await new Promise((resolve) => {
    const ws = new WebSocket(`wss://${location.host}/api/v1/wall/cable`)
    const identifier = JSON.stringify({ channel: 'WallChannel' })
    const done = () => { try { ws.close() } catch { /* already closed */ } resolve(seen) }

    ws.onmessage = (event) => {
      const msg = JSON.parse(event.data)
      if (msg.type === 'welcome') {
        ws.send(JSON.stringify({ command: 'subscribe', identifier }))
        return
      }
      if (msg.type === 'reject_subscription') { seen.rejected = true; return done() }
      if (msg.type === 'confirm_subscription') {
        seen.confirmed = true
        ws.send(JSON.stringify({
          command: 'message',
          identifier,
          data: JSON.stringify({ action: 'request_frames', variant: 'thumb', ids }),
        }))
        return
      }
      if (msg.message) {
        if (msg.message.frame) seen.frames++
        if (msg.message.error) seen.errors.push(msg.message.error)
      }
    }
    ws.onerror = () => done()
    setTimeout(done, 6000)
  })
}, { ids })

if (result.rejected) ok('the subscription was refused outright')
else if (result.confirmed) bad('the subscription was accepted without a grant')

if (result.frames === 0) {
  ok(`no frames reached a client that never rendered a page (asked for ${ids.length})`)
} else {
  bad(`${result.frames} frames were served to a client that never rendered a page`)
}

await browser.close()
console.log(failures === 0 ? '\nALL PASS' : `\n${failures} FAILED`)
process.exit(failures === 0 ? 0 : 1)

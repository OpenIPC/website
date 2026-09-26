// Can a client that never holds a grant take frames?
//
//   PW=$(ssh -p 35242 root@openipc.org 'cat /srv/www/.dev-basic-auth-password')
//   docker run --rm --network host -v "$PWD/tools":/w -w /w \
//     mcr.microsoft.com/playwright:v1.49.1-noble \
//     sh -c 'npm i -s playwright@1.49.1 >/dev/null; \
//            node bare-socket-check.mjs https://dev.openipc.org openipc '"$PW"
//
// WHY THIS EXISTS. #267 moved frames onto the socket so that a crawler
// collecting URLs would find nothing to collect, and no address returns a
// frame any more. Measured in the first four hours after that shipped, 991 of
// the 1,055 addresses on the socket had requested NOTHING else -- no page, no
// asset, no favicon, only the socket. They took 2,393 MiB of the 2,437. They
// were not driving browsers; they were speaking the socket's protocol
// directly, which needs no page at all.
//
// That is the client this simulates. It reads real frame ids from the wall's
// JSON -- an id is public and guessing was never the hard part -- then opens
// the socket by hand without a grant and asks for those frames by id.
//
// The Go tests prove the rule in-process; this proves what an outsider can
// reach through nginx on the real deployment.

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

// Real ids, taken from the wall's JSON the way anyone can.
const res = await page.request.get(`${base}/api/v1/wall/page/1.json`)
const ids = res.ok() ? (await res.json()).tiles.map((t) => t.id).slice(0, 8) : []
if (ids.length === 0) {
  console.error('no frames on the wall -- seed a camera day on dev first')
  process.exit(2)
}
ok(`read ${ids.length} real frame ids from the wall's JSON`)

// A same-origin document that runs none of the site's own code, so no grant
// is ever fetched or sent from this page.
await page.goto(`${base}/robots.txt`, { waitUntil: 'domcontentloaded' })

// Speak the protocol by hand, as the fleet does, with no grant.
const result = await page.evaluate(async ({ ids }) => {
  const seen = { frames: 0, errors: [], hello: false }

  return await new Promise((resolve) => {
    const ws = new WebSocket(`wss://${location.host}/api/v1/wall/socket`)
    const done = () => { try { ws.close() } catch { /* already closed */ } resolve(seen) }

    ws.onmessage = (event) => {
      const msg = JSON.parse(event.data)
      if (msg.type === 'hello') {
        seen.hello = true
        ws.send(JSON.stringify({ type: 'request', variant: 'thumb', ids }))
        return
      }
      if (msg.type === 'frame') seen.frames++
      if (msg.type === 'error') seen.errors.push(msg.error)
    }
    ws.onerror = () => done()
    setTimeout(done, 6000)
  })
}, { ids })

if (!result.hello) bad('the socket never said hello, so this probe proves nothing')
else if (result.errors.includes('no grant')) ok('the request was refused: no grant')

if (result.frames === 0) {
  ok(`no frames reached a client that holds no grant (asked for ${ids.length})`)
} else {
  bad(`${result.frames} frames were served to a client that holds no grant`)
}

await browser.close()
console.log(failures === 0 ? '\nALL PASS' : `\n${failures} FAILED`)
process.exit(failures === 0 ? 0 : 1)

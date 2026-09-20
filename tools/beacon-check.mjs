// Does the analytics beacon count each page, or once per session?
//
// The test suite can assert the script tag is present, same-origin and ordered
// after the bundle. It cannot assert what a browser does with it, and the
// whole risk in #181 lives there: Turbo Drive means the document `load` event
// fires once per SESSION, so a beacon left on its default behaviour records a
// single page view however far a visitor reads, and looks like it is working.
//
// This drives a real Chromium against dev and watches the network.
//
//   PW=$(ssh <origin> 'cat /srv/www/.dev-basic-auth-password')
//   docker run --rm --network host -v "$PWD/tools":/w -w /w \
//     mcr.microsoft.com/playwright:v1.49.1-noble \
//     sh -c 'npm i -s playwright@1.49.1 >/dev/null; \
//            node beacon-check.mjs https://dev.openipc.org openipc '"$PW"
//
// Install the package inside rather than mounting the host's node_modules:
// Playwright refuses a version that does not match the image's browsers.
import { chromium } from 'playwright'

const [, , base, user, pass] = process.argv
const results = []
const check = (name, ok, detail = '') => {
  results.push({ name, ok, detail })
  console.log(`  ${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? '  — ' + detail : ''}`)
}

const browser = await chromium.launch()
// WHAT THIS TOOL CAN AND CANNOT PROVE, because it cost an hour to establish.
//
// It proves the wiring: that a count is sent for the first page, for a Turbo
// navigation, and for a restoration visit, that each names its own page, that
// every one is accepted, and that nothing goes off-site.
//
// It CANNOT prove the hit is stored, and it must not be changed until it does.
// GoatCounter drops automated browsers, and count.js reports the browser it is
// running in: from a Playwright Chromium it sends `b=153`, a client-side bot
// flag, and the server answers 200 and discards the hit. Overriding the user
// agent below does not change that -- the client hint still says
// `sec-ch-ua: "HeadlessChrome"` and the flag is set from the page, not the
// header. Verified by replaying one captured URL twice against dev:
//
//   .../count?p=/replay-with-b&...&b=153   -> 200, not stored
//   .../count?p=/replay-no-b&...           -> 200, stored
//
// So a 200 here means accepted, never recorded, and the storage half was
// confirmed separately with a request that is not from an automated browser.
// Defeating the bot detection to make this tool's hits count would put
// synthetic traffic into the site's real figures, which is the opposite of
// what #181 is for.
//
// The user agent is still overridden, because the default one makes even the
// network path behave unlike a visitor's.
const UA = 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 ' +
           '(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'

const context = await browser.newContext({
  httpCredentials: { username: user, password: pass },
  userAgent: UA
})
const page = await context.newPage()

// Every request the page makes, so a third-party call cannot hide among them.
// Responses as well as requests: a count that is sent and refused is not a
// count, and the difference is invisible from the request side.
const counts = []
const external = []
const refused = []
page.on('request', req => {
  const url = new URL(req.url())
  if (url.hostname !== new URL(base).hostname) external.push(url.origin)
})
page.on('response', res => {
  const url = new URL(res.url())
  if (url.pathname !== '/api/a/count') return
  counts.push(url.searchParams.get('p'))
  if (!res.ok()) refused.push(`${url.searchParams.get('p')} -> ${res.status()}`)
})

// Waiting for the count itself, not for networkidle. turbo:load fires after
// the visit settles, so the beacon request can be the last thing to happen and
// networkidle resolves before it -- which made an earlier version of this tool
// report a working beacon as broken.
const counted = async (path, action) => {
  const wait = page.waitForResponse(
    res => new URL(res.url()).pathname === '/api/a/count', { timeout: 10000 }
  ).catch(() => null)
  await action()
  return (await wait) !== null
}

check('the first page is counted', await counted('/', () => page.goto(base)),
      `counts: ${JSON.stringify(counts)}`)

// A Turbo visit, which is where the load event would not fire again.
// Driven through Turbo.visit rather than a click: the navbar collapses at the
// default viewport, so the links are there but not clickable, and what is
// under test is the navigation, not the menu.
const hasTurbo = await page.evaluate(() => typeof window.Turbo?.visit === 'function')
check('Turbo Drive is actually loaded', hasTurbo,
      hasTurbo ? '' : 'without it every navigation is a full load and this test proves nothing')

check('a Turbo navigation is counted too',
      await counted('/donate', () => page.evaluate(() => window.Turbo.visit('/donate'))),
      `counts: ${JSON.stringify(counts)}`)
check('each count names the page it is for',
      counts.length === 2 && counts[0] !== counts[1], JSON.stringify(counts))

// Going back is a restoration visit: the scripts do not re-run, but turbo:load
// fires, so it must still count.
// A restoration visit: Turbo serves the cached body and does not re-run the
// scripts, so a beacon wired to anything but turbo:load misses it.
check('going back is counted', await counted('/', () => page.goBack()),
      `counts: ${JSON.stringify(counts)}`)

check('every count was accepted', refused.length === 0,
      refused.length ? refused.join(', ') : `${counts.length} counts, all 2xx`)

check('nothing is sent off-site', external.length === 0,
      external.length ? [...new Set(external)].join(' ') : 'no third-party requests')

const cookies = await context.cookies()
check('the beacon sets no cookie',
      !cookies.some(c => /goat|count|analytic|_ga/i.test(c.name)),
      cookies.map(c => c.name).join(', ') || 'no cookies at all')

await browser.close()
const failed = results.filter(r => !r.ok)
console.log(`\n${results.length - failed.length}/${results.length} passed`)
process.exit(failed.length ? 1 : 0)

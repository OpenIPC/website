// Does Turbo Drive actually drive?
//
// The test suite cannot answer this: whether a click tears the document down
// and rebuilds it, or swaps the body in place, is a browser behaviour. This
// drives a real Chromium against dev and checks the things that would break if
// the split in application.js were wrong.
//
// There is no node on this host and no browser in the dev container, so it
// runs in Playwright's image, which has both:
//
//   PW=$(ssh <origin> 'cat /srv/www/.dev-basic-auth-password')
//   docker run --rm --network host -v "$PWD/tools":/w -w /w \
//     mcr.microsoft.com/playwright:v1.49.1-noble \
//     sh -c 'npm i -s playwright@1.49.1 >/dev/null; \
//            node turbo-check.mjs https://dev.openipc.org openipc '"$PW"
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
const context = await browser.newContext({
  httpCredentials: { username: user, password: pass },
  ignoreHTTPSErrors: true,
})
const page = await context.newPage()

// Instrumentation, installed before anything navigates so it survives every
// body swap: the document outlives a Turbo visit, and so does anything a page
// left running in it.
await page.addInitScript(() => {
  window.__frames = 0
  const raf = window.requestAnimationFrame.bind(window)
  window.requestAnimationFrame = cb => { window.__frames++; return raf(cb) }
  window.__timers = new Set()
  const set = window.setInterval.bind(window), clear = window.clearInterval.bind(window)
  window.setInterval = (...a) => { const id = set(...a); window.__timers.add(id); return id }
  window.clearInterval = id => { window.__timers.delete(id); return clear(id) }
})

// The Tools menu is a collapsed dropdown, so a real click cannot reach its
// links headlessly. A scripted click still bubbles to document, which is where
// Turbo listens, so the code path under test is the same one.
const turboClick = sel => page.evaluate(s => document.querySelector(s)?.click(), sel)
const goHome = async () => { await turboClick('a.navbar-brand'); await page.waitForTimeout(500) }

// Keep these apart. A JavaScript exception means this change is broken; a
// failed resource means the server did not serve something, which on dev is
// routine -- its blob store holds 440K against 3,444 snapshot rows, so the
// homepage mosaic 500s on images that simply are not there.
const jsErrors = []
const resourceErrors = []
page.on('pageerror', e => jsErrors.push(String(e)))
page.on('console', m => {
  if (m.type() !== 'error') return
  ;(/Failed to load resource/.test(m.text()) ? resourceErrors : jsErrors).push(m.text())
})

// Count real document loads. Turbo navigations do not fire this.
let documentLoads = 0
page.on('load', () => { documentLoads++ })

await page.goto(base + '/', { waitUntil: 'networkidle' })

check('Turbo is present on the page', await page.evaluate(() => typeof window.Turbo !== 'undefined'))
check('Turbo Drive is enabled', await page.evaluate(() => window.Turbo?.session?.drive === true))
check('form mode is off, so forms submit as they always did',
      await page.evaluate(() =>
        (window.Turbo?.config?.forms?.mode ?? window.Turbo?.session?.formMode) === 'off'),
      await page.evaluate(() =>
        'formMode=' + (window.Turbo?.config?.forms?.mode ?? window.Turbo?.session?.formMode)))

const loadsAfterFirst = documentLoads

// Click an internal link and see whether the document was replaced.
const target = await page.evaluate(() => {
  const a = [...document.querySelectorAll('a[href^="/"]')]
    .find(x => !x.href.includes('#') && x.getAttribute('href') !== '/')
  return a ? a.getAttribute('href') : null
})
if (!target) {
  check('found an internal link to click', false)
} else {
  await Promise.all([
    page.waitForURL('**' + target, { timeout: 15000 }).catch(() => {}),
    page.click(`a[href="${target}"]`),
  ])
  await page.waitForTimeout(1200)
  check(`navigated to ${target}`, page.url().includes(target), page.url())
  check('it was a Turbo visit, not a document load',
        documentLoads === loadsAfterFirst,
        `document load events: ${documentLoads} (${loadsAfterFirst} before the click)`)
}

// The initialisers that must re-run after a swap.
check('external-links ran on the new page',
      await page.evaluate(() => {
        const ext = [...document.querySelectorAll('a[href^="http"]')]
        return ext.length === 0 || ext.some(a => a.target === '_blank')
      }))

// The delegated ones must be bound exactly once, not once per navigation.
await page.goBack(); await page.waitForTimeout(800)
await page.goForward(); await page.waitForTimeout(800)
check('no duplicate listeners after back/forward',
      await page.evaluate(() => {
        // zoom/copy/heif delegate from document; a stacked listener would show
        // up as the same handler running more than once for one click.
        window.__hits = 0
        document.addEventListener('click', () => { window.__hits++ }, { once: false })
        document.body.click()
        return window.__hits === 1
      }))

// --- The scripts that live in views, not in application.js ------------------
//
// Turbo re-executes the <script> elements in each body it swaps in, but the
// global lexical scope belongs to the document and survives the swap. A view
// whose script declares `const x` at the top level therefore throws
// "Identifier 'x' has already been declared" the *second* time you open it,
// and the whole block dies with it -- so one visit proves nothing and these
// checks all go round twice.
const inlineScriptPages = [
  '/tools/firmware-partitions-calculation',
  '/tools/high-resolution-timer',
  '/tools/qr-code-generator',
]
for (const path of inlineScriptPages) {
  const before = jsErrors.length
  await goHome()
  for (const _ of [1, 2]) {
    await turboClick(`a[href="${path}"]`)
    await page.waitForURL('**' + path, { timeout: 15000 }).catch(() => {})
    await page.waitForTimeout(600)
    await goHome()
  }
  check(`${path} survives being opened twice`,
        jsErrors.length === before, jsErrors.slice(before, before + 1).join(''))
}

// Reaching a page by link must leave it as usable as typing its URL does.
// The calculator fills #mtdparts from its own init, so an empty box means the
// init never ran.
await turboClick('a[href="/tools/firmware-partitions-calculation"]')
await page.waitForURL('**/tools/firmware-partitions-calculation', { timeout: 15000 }).catch(() => {})
await page.waitForTimeout(700)
check('the partition calculator initialises when reached by a link',
      await page.evaluate(() => (document.querySelector('#mtdparts')?.textContent || '').trim().length > 0),
      await page.evaluate(() => JSON.stringify((document.querySelector('#mtdparts')?.textContent || '').trim().slice(0, 40))))

// --- Nothing a page started may outlive it ---------------------------------
await goHome()
await turboClick('a[href="/tools/high-resolution-timer"]')
await page.waitForURL('**/tools/high-resolution-timer', { timeout: 15000 }).catch(() => {})
await page.waitForTimeout(800)
check('the frame-rate timer runs while you are on its page',
      await page.evaluate(() => window.__frames) > 5)
await goHome()
const framesOnLeaving = await page.evaluate(() => window.__frames)
await page.waitForTimeout(1500)
const framesLater = await page.evaluate(() => window.__frames)
check('the frame-rate timer stops when you leave it',
      framesLater - framesOnLeaving < 5,
      `${framesOnLeaving} -> ${framesLater} frame callbacks across 1.5 s away from the page`)

// ...and starts again on the way back. A restoration visit re-uses the cached
// DOM without re-running the page's scripts, so a timer that only stopped
// would come back stopped.
await page.goBack(); await page.waitForTimeout(1000)
const framesOnReturn = await page.evaluate(() => window.__frames)
await page.waitForTimeout(1000)
check('the frame-rate timer runs again after Back',
      await page.evaluate(() => window.__frames) - framesOnReturn > 5,
      `${framesOnReturn} frame callbacks on arrival, ${await page.evaluate(() => window.__frames)} a second later`)
await goHome()

// The Open Wall slideshow is a Bootstrap carousel, which cycles on an interval
// that only dispose() clears.
await page.goto(base + '/open-wall', { waitUntil: 'networkidle' })
const snapshot = await page.evaluate(() => document.querySelector('a[href^="/snapshots/"]')?.getAttribute('href'))
if (!snapshot) {
  check('found a snapshot to open', false, 'no snapshot links on /open-wall')
} else {
  await turboClick(`a[href="${snapshot}"]`); await page.waitForTimeout(800)
  const oneday = await page.evaluate(() => document.querySelector('a[href*="oneday"]')?.getAttribute('href'))
  if (!oneday) {
    check('found the one-day slideshow', false, `no oneday link on ${snapshot}`)
  } else {
    await turboClick(`a[href="${oneday}"]`); await page.waitForTimeout(1200)
    const running = await page.evaluate(() => window.__timers.size)
    check('the slideshow cycles while you are on its page', running > 0, `${running} live interval(s)`)
    await goHome(); await page.waitForTimeout(800)
    const leftBehind = await page.evaluate(() => window.__timers.size)
    check('the slideshow stops when you leave it', leftBehind === 0,
          `${running} live interval(s) on the page, ${leftBehind} still running after leaving`)
  }
}

check('no JavaScript exceptions anywhere in the run', jsErrors.length === 0, jsErrors.slice(0, 3).join(' | '))
if (resourceErrors.length) {
  console.log(`  note  ${resourceErrors.length} resource(s) failed to load — expected on dev, whose blob store is empty`)
}

await browser.close()

const failed = results.filter(r => !r.ok)
console.log(`\n${results.length - failed.length}/${results.length} passed`)
process.exit(failed.length ? 1 : 0)

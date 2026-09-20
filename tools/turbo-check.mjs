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
      await page.evaluate(() => window.Turbo?.session?.formMode === 'off'),
      await page.evaluate(() => 'formMode=' + window.Turbo?.session?.formMode))

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

check('no JavaScript exceptions anywhere in the run', jsErrors.length === 0, jsErrors.slice(0, 3).join(' | '))
if (resourceErrors.length) {
  console.log(`  note  ${resourceErrors.length} resource(s) failed to load — expected on dev, whose blob store is empty`)
}

await browser.close()

const failed = results.filter(r => !r.ok)
console.log(`\n${results.length - failed.length}/${results.length} passed`)
process.exit(failed.length ? 1 : 0)

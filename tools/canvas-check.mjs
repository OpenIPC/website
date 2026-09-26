// Do the Open Wall's frames actually reach a canvas, and is the page free of
// image addresses?
//
//   PW=$(ssh -p 35242 root@openipc.org 'cat /srv/www/.dev-basic-auth-password')
//   docker run --rm --network host -v "$PWD/tools":/w -w /w \
//     mcr.microsoft.com/playwright:v1.49.1-noble \
//     sh -c 'npm i -s playwright@1.49.1 >/dev/null; \
//            node canvas-check.mjs https://dev.openipc.org openipc '"$PW"
//
// Install the package inside rather than mounting the host's node_modules:
// Playwright refuses a version that does not match the image's browsers.
//
// WHY THIS EXISTS. Since 2026-09-23 no address on this site returns image
// bytes; frames arrive over the wall socket and are painted. Both halves of that
// need checking and neither is visible to the Go tests: they can assert
// the markup carries no image URL, but only a browser can say whether a frame
// arrived and was drawn. A silent failure here looks like an empty gallery,
// which is also what an empty database looks like -- so the check reads
// PIXELS, not a flag the page could set wrongly.
//
// Note dev usually has no cameras in the last 24 hours, so seed one before
// running this there or every canvas is legitimately blank.

import { chromium } from 'playwright'

const [base, user, pass] = process.argv.slice(2)
if (!base) {
  console.error('usage: node canvas-check.mjs <base-url> [user] [pass]')
  process.exit(2)
}

let failed = 0
const note = (ok, msg) => { if (!ok) failed++; console.log(`${ok ? 'PASS' : 'FAIL'}  ${msg}`) }

const browser = await chromium.launch()
const ctx = await browser.newContext({
  viewport: { width: 1280, height: 1000 },
  deviceScaleFactor: 2,
  httpCredentials: user ? { username: user, password: pass } : undefined,
})
const page = await ctx.newPage()

const errors = []
page.on('pageerror', (e) => errors.push(e.message))

async function check(path, label) {
  await page.goto(`${base}${path}`, { waitUntil: 'networkidle' })
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight))

  const canvases = await page.locator('canvas[data-wall-frame]').count()
  if (canvases === 0) {
    console.log(`SKIP  ${label}: no frames on the page (an empty wall looks like this too)`)
    return
  }

  // Painted is decided by reading the pixels back, because every other signal
  // -- a dataset flag, a drawImage counter -- is something the page sets and
  // could set while painting nothing.
  let painted = 0
  try {
    await page.waitForFunction(() => Array.from(document.querySelectorAll('canvas[data-wall-frame]'))
      .some((c) => {
        try {
          const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data
          for (let i = 3; i < d.length; i += 4000) if (d[i] !== 0) return true
        } catch { /* tainted or zero-sized */ }
        return false
      }), null, { timeout: 15000 })
  } catch { /* leave painted at 0 and report */ }

  painted = await page.evaluate(() => Array.from(document.querySelectorAll('canvas[data-wall-frame]'))
    .filter((c) => {
      try {
        const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data
        for (let i = 3; i < d.length; i += 4000) if (d[i] !== 0) return true
      } catch { /* tainted or zero-sized */ }
      return false
    }).length)

  note(painted > 0, `${label}: ${painted} of ${canvases} frames painted`)

  const html = await page.content()
  for (const needle of ['/wall/', '/rails/active_storage', '/download', 'camera.jpg']) {
    note(!html.includes(needle), `${label}: markup does not name ${needle}`)
  }

  const frameImgs = await page.evaluate(() => Array.from(document.querySelectorAll('img'))
    .map((i) => i.getAttribute('src') || '')
    .filter((src) => /wall|snapshot|active_storage/.test(src)))
  note(frameImgs.length === 0, `${label}: no <img> carries a frame${frameImgs.length ? ` (${frameImgs.join(', ')})` : ''}`)
}

await check('/open-wall', 'gallery')
await check('/', 'homepage mosaic')

// One camera's own pages, found from the gallery rather than hard-coded: ids
// live two days, so any literal in this file would be stale by the time it ran.
const href = await page.locator('a[href*="/snapshots/"]').first().getAttribute('href').catch(() => null)
if (href) {
  await check(href, 'snapshot page')
  await check(`${href}/oneday`, 'slideshow')
}

note(errors.length === 0, `no JavaScript exceptions${errors.length ? `: ${[...new Set(errors)].join(' | ')}` : ''}`)

await browser.close()
process.exit(failed === 0 ? 0 : 1)

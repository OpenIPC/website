// Does every frame on a wall page eventually paint?
//
//   docker run --rm --network host -v "$PWD/tools":/w -w /w \
//     mcr.microsoft.com/playwright:v1.49.1-noble \
//     sh -c 'npm i -s playwright@1.49.1 >/dev/null; \
//            node wall-fills-check.mjs https://openipc.org'
//
// WHY THIS EXISTS SEPARATELY FROM canvas-check.mjs. That check asks whether
// the transport works at all, and its bar is `painted > 0` -- it waits for the
// FIRST canvas and counts immediately, so a page that paints one frame of
// seventy-nine passes it. That was the right bar when the question was whether
// bytes could reach a canvas.
//
// Since #272 it is no longer sufficient. The channel now refuses any frame the
// page did not authorise, so a grant that covers less than the page drew --
// a surface that forgets to emit one, a cap like WallGrant::MAX_IDS quietly
// truncating a long page -- produces a wall that paints SOME frames and stops.
// Every automated check would stay green and a reader would see holes.
//
// So this one waits for the count to stop rising and then insists on all of
// them.

import { chromium } from 'playwright'

const [base, user, pass] = process.argv.slice(2)
if (!base) {
  console.error('usage: node wall-fills-check.mjs <base-url> [user] [pass]')
  process.exit(2)
}

const PAGES = ['/open-wall']

let failures = 0
const browser = await chromium.launch()
const context = await browser.newContext(
  user ? { httpCredentials: { username: user, password: pass } } : {},
)
const page = await context.newPage()

const countPainted = () => page.evaluate(() => {
  const canvases = Array.from(document.querySelectorAll('canvas[data-wall-frame]'))
  const painted = canvases.filter((c) => {
    try {
      const d = c.getContext('2d').getImageData(0, 0, Math.min(8, c.width), Math.min(8, c.height)).data
      return d.some((v, i) => (i % 4 === 3 ? v !== 0 : v !== 0))
    } catch { return false }
  })
  return { total: canvases.length, painted: painted.length }
})

// Follow the biggest surfaces a reader can reach, not just the landing page.
await page.goto(`${base}/open-wall`, { waitUntil: 'networkidle' })
const first = await page.$eval('[data-wall-frame]', (el) => el.dataset.wallFrame).catch(() => null)
if (first) {
  PAGES.push(`/snapshots/${first}`)
  PAGES.push(`/snapshots/${first}/oneday`)
}

for (const path of PAGES) {
  await page.goto(`${base}${path}`, { waitUntil: 'networkidle' })

  // Settle: poll until the painted count has stopped moving for a good while.
  //
  // STABLE has to be generous. A page requests in chunks 250 ms apart and a
  // production fullhd frame is a couple of hundred kilobytes, so the count
  // pauses repeatedly on its way up. At three rounds this reported 71 of 79
  // against production while the same code painted 79 of 79 against dev, whose
  // seeded frames are a couple of kilobytes and arrive instantly -- a
  // measurement artefact that reads exactly like frames being refused.
  const STABLE = 12 // × 500 ms = six seconds of no movement
  let last = -1
  let stable = 0
  let seen = { total: 0, painted: 0 }
  for (let i = 0; i < 180 && stable < STABLE; i++) {
    await page.waitForTimeout(500)
    seen = await countPainted()
    if (seen.painted === last) stable++
    else { stable = 0; last = seen.painted }
  }

  if (seen.total === 0) {
    console.log(`SKIP  ${path}: no frames on this page`)
    continue
  }
  const ok = seen.painted === seen.total
  if (!ok) failures++
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${path}: ${seen.painted} of ${seen.total} frames painted`)
}

await browser.close()
console.log(failures === 0 ? '\nALL PASS' : `\n${failures} FAILED`)
process.exit(failures === 0 ? 0 : 1)

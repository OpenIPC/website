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
// a surface that forgets to emit one, a cap on a grant's pairs quietly
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

  // Wait for the count the page itself declares, not for stillness.
  //
  // Settling on "the number stopped moving" was wrong twice over. A page asks
  // in chunks 250 ms apart and a production fullhd frame is a couple of
  // hundred kilobytes, so the count pauses repeatedly on the way up: at three
  // quiet rounds this reported 71 of 79 against production while the same code
  // painted 79 of 79 against dev, whose seeded frames are two kilobytes and
  // arrive instantly. Widening the window to six seconds only moved the
  // threshold -- production reported 71 again on a slower run, and the server
  // log confirmed the cause was this checker giving up and closing the page
  // while the last chunk was still queued. The measurement was killing the
  // thing it measured.
  //
  // The page knows how many canvases it has, so wait for that number and stop
  // early when it arrives. Only a genuine shortfall now takes the full
  // deadline.
  // Wait while PROGRESS is being made, not for a fixed period and not for
  // stillness. Both of those were tried against production and both lied.
  //
  // Stillness is too eager: a page asks in chunks 250 ms apart, so the count
  // pauses on the way up, and at six quiet seconds this reported 71 of 79 --
  // indistinguishable from frames being refused. A flat deadline is arbitrary:
  // at ninety seconds it reported 71 of 79 again, on a camera whose frames are
  // 700 KB each, because the full set is about 75 MB down one socket. The same
  // check said 79 of 79 on a camera with smaller frames, so the tool's verdict
  // depended on which camera it happened to pick.
  //
  // Both times the conclusion "frames are being refused" was wrong, and each
  // cost a round of digging through server logs to disprove. So: keep waiting
  // while the number is still climbing, give up only when it has genuinely
  // stalled, and cap the whole thing so a broken page cannot hang a run.
  // The TOTAL moves too, and forgetting that would let this pass a wall it
  // never checked. A lazy turbo-frame arrives after the page does: the snapshot
  // page starts with its strip and gains an archive, the one-day page starts
  // with one canvas and gains seventy-nine slides. Exiting the moment
  // `painted === total` would therefore declare victory over whichever handful
  // was present at the first sample, and never look at the frames the lazy
  // frame brought -- passing on exactly the surface most likely to be broken,
  // since those frames depend on a grant emitted inside the frame.
  //
  // So completion needs both halves: everything painted, AND the canvas count
  // holding still long enough that a pending frame would have landed.
  const STALL_MS = 30_000
  const SETTLED_MS = 5_000
  const CAP_MS = 300_000
  const started = Date.now()

  let seen = await countPainted()
  let lastPainted = seen.painted
  let lastTotal = seen.total
  let lastChange = Date.now()

  const done = () => seen.total > 0 &&
                     seen.painted === seen.total &&
                     Date.now() - lastChange >= SETTLED_MS

  while (!done() && Date.now() - lastChange < STALL_MS && Date.now() - started < CAP_MS) {
    await page.waitForTimeout(500)
    seen = await countPainted()
    if (seen.painted !== lastPainted || seen.total !== lastTotal) {
      lastPainted = seen.painted
      lastTotal = seen.total
      lastChange = Date.now()
    }
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

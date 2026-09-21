// Does the what-next list actually appear once per tab? (#191)
//
// The suite asserts the markup is there and which ask it carries. Whether the
// browser then hides it on the second camera of a session is sessionStorage
// behaviour, and the failure is silent in the direction that matters: a list
// that keeps reappearing looks exactly like a list that is working, to anyone
// who only opens one page.
//
//   PW=$(ssh <origin> 'cat /srv/www/.dev-basic-auth-password')
//   docker run --rm --network host -v "$PWD/tools":/w -w /w \
//     mcr.microsoft.com/playwright:v1.49.1-noble \
//     sh -c 'npm i -s playwright@1.49.1 >/dev/null; \
//            node whatnext-check.mjs https://dev.openipc.org openipc '"$PW"
import { chromium } from 'playwright'

const [, , base, user, pass] = process.argv
const results = []
const check = (name, ok, detail = '') => {
  results.push(ok)
  console.log(`  ${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? '  — ' + detail : ''}`)
}

const params =
  'camera%5Bflash_type%5D=nor16m&camera%5Bfirmware_version%5D=ultimate' +
  '&camera%5Bnetwork_interface%5D=eth&camera%5Bsd_card_slot%5D=nosd'
const wizard = chip => `${base}/cameras/vendors/${chip}?${params}`

const b = await chromium.launch()
const c = await b.newContext({ httpCredentials: { username: user, password: pass } })
const p = await c.newPage()

// Visible means visible, not merely present: `hidden` is what the page sets,
// and asserting on the attribute would pass even if CSS overrode it.
const listVisible = async () => {
  const el = p.locator('[data-whatnext]')
  return (await el.count()) ? el.first().isVisible() : false
}

await p.goto(wizard('sigmastar/socs/ssc338q'), { waitUntil: 'networkidle' })
check('the first camera of a tab is offered the list', await listVisible())

const asks = await p.locator('[data-whatnext] a[data-event]').evaluateAll(
  els => els.map(e => e.dataset.event))
check('three things to do next', asks.length === 3, asks.join(', '))
check('exactly one ask about money',
      asks.filter(e => /donate|business/.test(e)).length === 1,
      asks.filter(e => /donate|business/.test(e)).join(', '))

// An FPV chip: the ask is commercial and the room is the FPV one.
check('an FPV chip is asked about commercial terms, not donations',
      asks.includes('download-step:business:fpv'))
const chat = await p.locator('[data-whatnext] a[data-event="download-step:chat"]').getAttribute('href')
check('and pointed at the FPV room', chat === 'https://t.me/+BMyMoolVOpkzNWUy', chat)

// Second camera, same tab. This is the whole reason the file exists.
await p.goto(wizard('goke/socs/gk7205v300'), { waitUntil: 'networkidle' })
check('the second camera of the same tab is not asked again', !(await listVisible()))

// Back, after a Turbo visit, in a tab that has not seen the list yet.
//
// Three things all have to be true for this to exercise anything, and the
// first two versions of this check got them wrong and passed against a build
// that was broken:
//
//   1. A *Turbo* visit, not page.goto(). goto() is a full browser navigation;
//      Back from one rebuilds the document, so no per-element state survives
//      and a DOM-cache bug cannot reproduce.
//   2. A *fresh* context. By this point in the file the session flag is
//      already set, so the list is hidden before Turbo ever caches it -- and
//      a hidden list coming back hidden proves nothing.
//   3. The list *visible* when the snapshot is taken, which is what (2) buys.
//
// Then Back restores that cached DOM, and whatever was written onto the list
// comes back with it. That is how a marker meant to last one page view lasted
// longer and made the ask reappear.
const backCtx = await b.newContext({ httpCredentials: { username: user, password: pass } })
const back = await backCtx.newPage()
await back.goto(wizard('sigmastar/socs/ssc338q'), { waitUntil: 'networkidle' })

const wasVisible = await back.locator('[data-whatnext]').first().isVisible()
check('the list is visible before the snapshot is taken', wasVisible,
      wasVisible ? '' : 'nothing below can reproduce a cache bug')

const visited = await back.evaluate(() => {
  if (!window.Turbo) return false
  const done = new Promise(r => document.addEventListener('turbo:load', () => r(true), { once: true }))
  window.Turbo.visit('/open-wall')
  return Promise.race([done, new Promise(r => setTimeout(() => r(false), 5000))])
})
check('the check actually made a Turbo visit', visited,
      visited ? '' : 'Turbo is not driving; the Back case below proves nothing')

// Waiting on turbo:load, not networkidle. A restoration visit that hits
// Turbo's cache makes no request at all, so networkidle resolves instantly and
// samples the DOM mid-restore -- where the element is simply absent. That
// reads as "not visible" and passes, which is how this check agreed with a
// build that had the bug.
await back.evaluate(() => {
  window.__restored = new Promise(r => document.addEventListener('turbo:load', () => r(), { once: true }))
})
await back.goBack()
await back.evaluate(() => window.__restored).catch(() => {})

const restored = await back.evaluate(() => !!document.querySelector('[data-whatnext]'))
check('the page actually came back', restored,
      restored ? '' : 'sampled mid-restore; the assertion below would be meaningless')

const stillAsking = await back.locator('[data-whatnext]').first().isVisible()
check('going back after a Turbo visit does not repeat the ask', !stillAsking)

// A different tab is a different person as far as this is concerned.
const fresh = await (await b.newContext({ httpCredentials: { username: user, password: pass } })).newPage()
await fresh.goto(wizard('ingenic/socs/t31l'), { waitUntil: 'networkidle' })
const freshVisible = await fresh.locator('[data-whatnext]').first().isVisible()
check('a new tab is offered it again', freshVisible)

const freshAsks = await fresh.locator('[data-whatnext] a[data-event]').evaluateAll(
  els => els.map(e => e.dataset.event))
check('a consumer chip is asked to donate, not for commercial terms',
      freshAsks.includes('download-step:donate') && !freshAsks.some(e => e.startsWith('download-step:business')),
      freshAsks.join(', '))

// What this cannot prove: that the events reach the counter. GoatCounter drops
// automated browsers client-side (b=153), so a click here is deliberately not
// counted and the dashboard is the only place that answers it.
await b.close()
console.log(`\n${results.filter(Boolean).length}/${results.length} passed`)
process.exit(results.every(Boolean) ? 0 : 1)

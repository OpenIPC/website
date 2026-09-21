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

// Back. Turbo restores the page from its DOM cache, so whatever state the
// list was left in comes back with it -- the first fix for the double-run bug
// marked the element as handled, and that marker was cached too, which made
// Back show the ask again. There is no marker now; turbo:load fires on a
// restore like any other navigation and the flag decides.
await p.goBack({ waitUntil: 'networkidle' })
check('going back does not repeat the ask', !(await listVisible()))

await p.goForward({ waitUntil: 'networkidle' })
check('and forward does not either', !(await listVisible()))

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

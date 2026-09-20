// Do the clicks actually count? (#183)
//
// The suite asserts the markup names an event. Whether a click emits one, and
// whether exactly one arrives, is browser behaviour -- and the failure mode is
// silent: a dashboard zero reads as "nobody clicked", not "nobody counted".
//
//   PW=$(ssh <origin> 'cat /srv/www/.dev-basic-auth-password')
//   docker run --rm --network host -v "$PWD/tools":/w -w /w \
//     mcr.microsoft.com/playwright:v1.49.1-noble \
//     sh -c 'npm i -s playwright@1.49.1 >/dev/null; \
//            node events-check.mjs https://dev.openipc.org openipc '"$PW"
import { chromium } from 'playwright'

const [, , base, user, pass] = process.argv
const results = []
const check = (name, ok, detail = '') => {
  results.push(ok)
  console.log(`  ${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? '  — ' + detail : ''}`)
}

const b = await chromium.launch()
const c = await b.newContext({
  httpCredentials: { username: user, password: pass },
  userAgent: 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'
})
const p = await c.newPage()

// Every count the page sends, with whether it was flagged as an event.
const counts = []
p.on('request', r => {
  const u = new URL(r.url())
  if (u.pathname === '/api/a/count') counts.push({ path: u.searchParams.get('p'), event: u.searchParams.get('e') })
})
// Outbound navigation must not actually happen in a test run.
await c.route('**://paywall.pw/**', r => r.abort())
await c.route('**://opencollective.com/**', r => r.abort())
await c.route('**://t.me/**', r => r.abort())

const clicked = async (path, selector) => {
  await p.goto(base + path, { waitUntil: 'networkidle' })
  const before = counts.length
  await p.click(selector, { modifiers: ['Control'] }).catch(() => {})  // keep the tab
  await p.waitForTimeout(900)
  return counts.slice(before).map(x => x.path)
}

check('a donate click counts oc-checkout',
      (await clicked('/donate', 'a[data-event="oc-checkout"]')).includes('oc-checkout'))
check('a Russian donate click counts paywall-checkout',
      (await clicked('/ru/donate', 'a[data-event="paywall-checkout"]')).includes('paywall-checkout'))
check('a community click counts tg-join',
      (await clicked('/community', 'a[data-event="tg-join"]')).includes('tg-join'))

// An unnamed outbound link falls back to its host. :visible matters -- the
// first GitHub link on any page is in the collapsed navbar, and clicking a
// hidden element fails silently and looks like the fallback not firing.
const ext = await clicked('/our-team', 'article a[href^="https://github.com"]:visible')
check('an unnamed outbound link counts by host', ext.some(e => e?.startsWith('ext:')), ext.join(' '))

// Exactly one event per click, not two.
const twice = await clicked('/donate', 'a[data-event="oc-checkout"]')
check('one click is one event', twice.filter(e => e === 'oc-checkout').length === 1, twice.join(' '))

// The landing tag: counted once, then gone from the address.
await p.goto(`${base}/?ref=tg-ru`, { waitUntil: 'networkidle' })
await p.waitForTimeout(900)
check('a landing tag is counted', counts.some(x => x.path === 'ref:tg-ru'))
check('the tag is removed from the address', !p.url().includes('ref='), p.url().replace(base, ''))
check('the page itself is counted without the tag',
      counts.some(x => x.path === '/'), counts.slice(-4).map(x => x.path).join(' '))

// Junk in the tag must not become a dashboard row.
await p.goto(`${base}/?ref=${encodeURIComponent('<script>x</script>')}`, { waitUntil: 'networkidle' })
await p.waitForTimeout(900)
check('a junk tag is not recorded at all',
      !counts.some(x => (x.path || '').startsWith('ref:script')),
      counts.slice(-3).map(x => x.path).join(' '))

await b.close()
const failed = results.filter(r => !r).length
console.log(`\n${results.length - failed}/${results.length} passed`)
process.exit(failed ? 1 : 0)

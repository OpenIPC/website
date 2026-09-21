// Screenshots of a change on dev, for the user to look at before a PR.
//
// Every visual change here has to be shown before it is proposed, and doing
// that by hand costs the same twenty minutes each time: launch a browser, find
// the basic-auth password, remember that a 1x capture looks soft on a HiDPI
// display, remember that a full-page shot of a long page is unreadable, and
// write the files somewhere the user can actually open them.
//
//   PW=$(ssh <origin> 'cat /srv/www/.dev-basic-auth-password')
//   docker run --rm --network host -v "$PWD/tools":/w -v "$HOME/reports":/out -w /w \
//     mcr.microsoft.com/playwright:v1.49.1-noble \
//     sh -c 'npm i -s playwright@1.49.1 >/dev/null; \
//            node shot.mjs https://dev.openipc.org openipc '"$PW"' /out/name=/path?query ...'
//
// Each target is `name=/path`. A target may add `#selector` to shoot one
// element rather than the page, which is what makes a band inside a long page
// legible instead of a stripe in a two-metre image. The selector is
// percent-decoded, so an id selector is written `#%23builds`: `#` is already
// the separator, and a bare `#builds` would arrive here as an empty string. The selector is
// percent-decoded, so an id selector is written `#%23builds` -- `#` is the
// separator, and a bare `#builds` would be read as an empty selector.
import { chromium } from 'playwright'

const [, , base, user, pass, ...targets] = process.argv
if (!targets.length) {
  console.error('usage: shot.mjs <base> <user> <pass> <out/name=/path[#selector]> ...')
  process.exit(2)
}

// 2x: these are read on HiDPI displays, and a 1x capture of 14px body text is
// the kind of soft that makes a reviewer squint at the design rather than at
// the change.
const b = await chromium.launch()
const c = await b.newContext({
  httpCredentials: { username: user, password: pass },
  viewport: { width: 1280, height: 900 },
  deviceScaleFactor: 2
})
const p = await c.newPage()

let failed = 0
for (const target of targets) {
  // On the FIRST '=' only. Splitting on every one truncated the URL at the
  // first query parameter, so `name=/wizard?camera[flash_type]=nor16m` opened
  // the wizard with no parameters at all and photographed its defaults -- the
  // shots looked right, and were of a different configuration than the one
  // asked for.
  const split = target.indexOf('=')
  const out = target.slice(0, split)
  const rest = target.slice(split + 1)
  const [path, raw] = rest.split('#')
  const selector = raw && decodeURIComponent(raw)
  const url = `${base}${path}`
  try {
    const r = await p.goto(url, { waitUntil: 'networkidle' })
    if (!r || !r.ok()) throw new Error(`HTTP ${r ? r.status() : 'no response'}`)

    // Fonts settle after networkidle often enough to matter: a shot taken
    // mid-swap shows the fallback stack, which reads as a broken design.
    await p.evaluate(() => document.fonts.ready)

    const file = `${out}.png`
    if (selector) {
      const el = p.locator(selector).first()
      if (!(await el.count())) throw new Error(`no element matching ${selector}`)
      await el.scrollIntoViewIfNeeded()
      await el.screenshot({ path: file })
    } else {
      await p.screenshot({ path: file, fullPage: true })
    }
    console.log(`  ok    ${file}  <- ${url}${selector ? ` [${selector}]` : ''}`)
  } catch (e) {
    failed++
    console.log(`  FAIL  ${out}  <- ${url}  — ${e.message}`)
  }
}

await b.close()
process.exit(failed ? 1 : 0)

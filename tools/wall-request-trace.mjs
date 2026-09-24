// What did the page ASK for, and what came BACK?
//
//   node wall-request-trace.mjs https://openipc.org /snapshots/<id>/oneday
//
// WHY THIS EXISTS. wall-fills-check.mjs says how many frames painted. When
// that number is short it cannot say why, and since #272 there are several
// silent ways for a frame not to arrive: the grant may not name it, the
// address may be over budget, the file may have been purged mid-request, or
// the socket may simply still be working. Three of those four leave nothing in
// the page and one leaves nothing in the log either.
//
// So this wraps WebSocket before the page's own script runs and records both
// sides: every id the client requested, every frame that came back, and any
// error the channel sent. `onPageNotAsked` and `askedNotAnswered` separate
// "the client never asked" from "the server never answered", which is the fork
// that matters and the one that took two wrong guesses to reach by other
// means.
//
// Read `verdict` before either list. A refusal from the channel names no
// frame, so an id refused for budget or for a missing grant is absent from the
// returned set in exactly the same way as one still in flight -- the bare
// difference cannot tell those apart, and it is the difference between "we are
// dropping frames" and "it had not finished".
//
// It answered exactly that on 2026-09-24: a slideshow reporting 71 of 79 had
// asked for all 79 and received 71 with no error, which ruled out the grant
// and the budget in one step and left throughput, which was the answer.

import { chromium } from 'playwright'

const [base, path] = process.argv.slice(2)
const browser = await chromium.launch()
const page = await browser.newPage()

await page.addInitScript(() => {
  window.__wall = { asked: new Set(), got: new Set(), messages: 0, errors: [] }
  const Native = window.WebSocket
  window.WebSocket = new Proxy(Native, {
    construct(target, args) {
      const ws = new target(...args)
      const send = ws.send.bind(ws)
      ws.send = (data) => {
        try {
          const m = JSON.parse(data)
          if (m.command === 'message') {
            const d = JSON.parse(m.data)
            if (d.action === 'request_frames') {
              window.__wall.messages++
              d.ids.forEach((i) => window.__wall.asked.add(`${i}:${d.variant}`))
            }
          }
        } catch { /* not ours */ }
        return send(data)
      }
      ws.addEventListener('message', (e) => {
        try {
          const m = JSON.parse(e.data)
          if (m.message && m.message.frame) window.__wall.got.add(`${m.message.id}:${m.message.variant}`)
          if (m.message && m.message.error) window.__wall.errors.push(m.message.error)
        } catch { /* ignore */ }
      })
      return ws
    },
  })
})

await page.goto(`${base}${path}`, { waitUntil: 'networkidle' })
await page.waitForTimeout(Number(process.env.TRACE_WAIT_MS || 150000))

const r = await page.evaluate(() => {
  const canvases = Array.from(document.querySelectorAll('canvas[data-wall-frame]'))
  const keys = canvases.map((c) => `${c.dataset.wallFrame}:${c.dataset.wallVariant}`)
  const asked = [...window.__wall.asked]
  const got = [...window.__wall.got]
  const missing = asked.filter((k) => !got.includes(k))
  const errors = window.__wall.errors

  // "Asked and not got" does NOT mean the server stayed silent.
  //
  // The channel answers a refusal with an error message that names no frame,
  // so an id refused for budget, for an invalid grant, for an unknown variant
  // or for an oversized request is absent from `got` in exactly the same way
  // as one still in flight. Reporting the bare difference sends whoever is
  // reading this down the silence branch when the server did in fact reply --
  // and telling those two apart is the only reason this tool exists.
  const verdict = missing.length === 0
    ? 'every id asked for came back'
    : errors.length > 0
      ? `the channel REFUSED this connection (${errors.join('; ')}) -- ` +
        'the missing ids were answered, not ignored; treat the error as the cause'
      : 'no error was sent, so the missing ids were either still in flight ' +
        'when this stopped watching, or silently filtered -- a grant that does ' +
        'not name them, or a file that could not be read. Re-run with a longer ' +
        'TRACE_WAIT_MS before concluding anything else.'

  return {
    canvases: canvases.length,
    distinctKeys: new Set(keys).size,
    messages: window.__wall.messages,
    asked: asked.length,
    got: got.length,
    verdict,
    errors: errors.slice(0, 5),
    askedNotAnswered: missing.slice(0, 10),
    onPageNotAsked: [...new Set(keys)].filter((k) => !asked.includes(k)).slice(0, 10),
    grants: document.querySelectorAll('[data-wall-grant]').length,
  }
})

console.log(JSON.stringify(r, null, 2))
await browser.close()

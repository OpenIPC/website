// First-party, cookieless page counting (#181).
//
// The origin runs GoatCounter on 127.0.0.1:8081 and nginx proxies exactly two
// paths to it, so every request here is same-origin and nothing reaches a
// third party. The site has had no analytics at all: the access log cannot see
// language, cannot see country, cannot tell whether 560 addresses on
// /open-wall were people or a proxy-checker, and 39% of human page views
// arrive with no referrer because Telegram and apps send none.
//
// Two things about this file are load-bearing.
//
// `no_onload` is set because Turbo Drive shipped in 72543b4. The document
// `load` event fires once per SESSION now, not once per page, so count.js
// counting on load would record a single page view however far the visitor
// reads. Counting on turbo:load instead records every page, including the
// first -- turbo:load fires on the initial render too, which is why the
// automatic count has to be off or the first page would be counted twice.
//
// The config lives in the layout rather than here, in an inline script that
// runs at parse time. Relying on this bundle to set it worked -- deferred
// scripts do run in document order -- but it made the guarantee depend on how
// the bundle is built, and the failure is silent: count.js would count the
// document load event instead, which Turbo fires once per session.
// One event. GoatCounter treats an event as a page view whose path is the
// event name, which is why these names are short and stable: they appear in
// the dashboard's list beside real paths and are read by the monthly memo.
export function countEvent(name) {
  window.goatcounter?.count?.({ path: name, event: true })
}

// A landing tag, recorded once and then removed from the address.
//
// The project posts links to itself -- Telegram pins, the firmware and wiki
// READMEs, YouTube descriptions, the camera WebUI's link home -- and 39% of
// human page views arrive with no referrer at all, so those channels are
// indistinguishable from someone typing the URL. ?ref=<tag> tells them apart.
//
// Stripped before the page is counted, not after, and that ordering is the
// whole point: left in place it would fragment the page report into /?ref=tg-ru,
// /?ref=readme and /?ref=yt, which is three rows saying what one row plus one
// event says better. replaceState rather than pushState so Back still leaves
// the site rather than stepping through a URL the visitor never chose.
function recordLandingTag() {
  const url = new URL(window.location.href)
  const tag = url.searchParams.get('ref')
  if (!tag) return

  // Recorded only if it was already a well-formed tag. Sanitising and then
  // recording the result would let anyone mint dashboard rows by appending
  // junk -- ?ref=<script>x</script> came through as ref:scriptxscript, which
  // is harmless but is still a row nobody asked for, sitting next to the real
  // channels in the report the monthly memo reads.
  //
  // The tags are the project's own (README, "Links the project posts"), so
  // anything that does not look like one is not one.
  const clean = tag.toLowerCase()
  if (/^[a-z0-9-]{1,24}$/.test(clean)) countEvent(`ref:${clean}`)

  url.searchParams.delete('ref')

  // history.state, not {}. Turbo keeps a restorationIdentifier in the history
  // entry and needs it to restore the cached body when someone presses Back;
  // replacing the entry with an empty object throws it away, and the failure
  // is horrible to read -- the address goes back and the page does not, so the
  // visitor is looking at /donate with / in the address bar. Reproduced on dev
  // before this line was written, and tools/events-check.mjs now walks it.
  window.history.replaceState(window.history.state, '', url.pathname + url.search + url.hash)
}

export default function initAnalytics() {
  // window.goatcounter is NOT set here. It is set by an inline script in the
  // layout, which runs at parse time and therefore before every deferred
  // script including this bundle -- so count.js cannot read it early whatever
  // the bundler does with this file. Setting it here as well would overwrite
  // the object count.js has already attached its count() to.

  // Optional chaining throughout: count.js is fetched from the network and a
  // blocked or failed request must not take the rest of the bundle down with
  // it. Analytics failing is not a reason for the page to stop working.
  document.addEventListener('turbo:load', () => {
    // Before the page count, so the address it reads is already clean.
    recordLandingTag()

    window.goatcounter?.count?.({
      // Explicit rather than left to count.js's default, which reads
      // location.pathname at the moment the script ran -- on a Turbo visit
      // that is the page the visitor arrived on, not the one being counted.
      path: window.location.pathname + window.location.search
    })
  })
}

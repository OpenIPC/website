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
// The config is assigned here, in the bundle, and the bundle is included
// BEFORE count.js in the layout. Both tags are `defer`, and deferred scripts
// run in document order, so this has always run by the time count.js looks for
// it. An `async` count.js would be a race: it could win, see no config, and
// count the load event.
export default function initAnalytics() {
  window.goatcounter = { no_onload: true, endpoint: '/api/a/count' }

  // Optional chaining throughout: count.js is fetched from the network and a
  // blocked or failed request must not take the rest of the bundle down with
  // it. Analytics failing is not a reason for the page to stop working.
  document.addEventListener('turbo:load', () => {
    window.goatcounter?.count?.({
      // Explicit rather than left to count.js's default, which reads
      // location.pathname at the moment the script ran -- on a Turbo visit
      // that is the page the visitor arrived on, not the one being counted.
      path: window.location.pathname + window.location.search
    })
  })
}

// The landing tag (#183, restored in #360).
//
// The project posts links to itself with ?ref=<tag> -- Telegram pins,
// READMEs, the camera WebUI's link home -- because many readers arrive with no
// referrer at all. The tag is recorded as one `ref:<tag>` event and stripped
// from the address before count.js counts the page, so the page stays one row
// in the report instead of one per channel. Only a well-formed tag is
// recorded, so a junk ?ref= cannot mint rows.
//
// Inlined LAST in the body by layouts/Base.astro, and both halves matter.
// Inline scripts run while the document parses and count.js is deferred until
// parsing ends, so this still strips the address before count.js reads it.
// And last, because the home page's language redirect (pages/index.astro) runs
// earlier in the body: it must see ?ref= to carry it to /ru or /zh, where this
// records it. `openipcLeaving` is that redirect saying the page is being left,
// so the tag is not counted on the way out as well. events.test.ts runs THIS
// file, not a copy of it.
(function () {
  var url = new URL(location.href);
  if (window.openipcLeaving || !url.searchParams.has('ref')) return;
  var tag = (url.searchParams.get('ref') || '').toLowerCase();
  url.searchParams.delete('ref');
  history.replaceState(history.state, '', url.pathname + url.search + url.hash);
  if (!/^[a-z0-9-]{1,24}$/.test(tag)) return;

  // The address no longer holds the tag, so this page is its only chance to
  // be counted. DOMContentLoaded is the first moment count.js has run --
  // deferred scripts run just before it -- and not `load`, which waits for
  // every image. pagehide covers a reader who leaves even sooner.
  var sent = false;
  function send() {
    if (sent) return;
    sent = true;
    window.openipcCount('ref:' + tag);
  }
  document.addEventListener('DOMContentLoaded', send);
  addEventListener('pagehide', send);
})();

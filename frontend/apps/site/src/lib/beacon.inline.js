// The beacon's settings and the click sender (#181, #183; restored in #360).
//
// Inlined into the head of every page by layouts/Base.astro (imported ?raw),
// so it runs while the page is still parsing -- before count.js, which is
// deferred, and before any link can be clicked. events.test.ts runs THIS
// file, not a copy of it.
//
// What it counts, and why by name: the business page ends in a mailto:, the
// donate page in a link to another host, the community page in Telegram
// invites. None of those touch the server, so the only record is a GoatCounter
// event, which the monthly memo (deploy/audience-memo.sh) reads by name. A
// missing event reads exactly like nobody clicking, which is how a whole week
// of them went uncounted without anything failing.
(function () {
  // count.js reads this object and attaches count() to it, so it is set
  // before count.js runs and never replaced afterwards.
  var gc = (window.goatcounter = { endpoint: '/api/a/count' });

  // count.js once it has run; until then the same request, sent directly.
  // count.js uses sendBeacon itself, so either way the count survives the
  // navigation that usually follows the click. The early request lacks the
  // title, screen and bot fields count.js adds; the memo reads events by
  // name (p=, e=true) and needs none of them.
  function count(name) {
    if (gc.count) return gc.count({ path: name, event: true });
    if (!navigator.sendBeacon) return;
    navigator.sendBeacon(gc.endpoint + '?p=' + encodeURIComponent(name) +
      '&e=true&rnd=' + Math.random().toString(36).slice(2, 7));
  }

  // A link that names its own event wins -- a list of selectors in here would
  // drift the moment someone moves a button. Any other link to another host
  // counts by host, so the long tail is visible without naming each one.
  function eventFor(link) {
    var named = link.getAttribute('data-event');
    if (named) return named;
    var href = link.getAttribute('href') || '';
    if (!/^https?:/i.test(href)) return null;
    var host;
    try { host = new URL(href).hostname; } catch (e) { return null; }
    return host === location.hostname ? null : 'ext:' + host;
  }

  // Delegated from the document, bound once: the wizard's download-step links
  // are rendered by a Preact island after load, and a per-link bind would
  // miss exactly the links the memo's funnels need.
  document.addEventListener('click', function (ev) {
    var link = ev.target && ev.target.closest ? ev.target.closest('a[href]') : null;
    var name = link && eventFor(link);
    if (name) count(name);
  });

  // For the landing tag at the end of the body.
  window.openipcCount = count;
})();

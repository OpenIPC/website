/**
 * Count the clicks that leave a page (#183, restored in #360).
 *
 * The business page ends in a mailto:, the donate page in a link to another
 * host, the community page in Telegram invites. None of those touch the
 * server, so the access log cannot see them; the only record is a GoatCounter
 * event, which the monthly memo (deploy/audience-memo.sh) reads by name.
 *
 * This lived in the Rails asset pipeline and went with Rails in f8d1a57
 * without a port, and the site counted no click from 26 September until it
 * came back. Nothing reported it: a missing event reads as nobody clicking.
 * site.build.test.ts now fails a build whose pages do not ship it.
 *
 * One listener, delegated from `document`. The wizard's download-step links
 * are rendered by a Preact island after load, so a per-element bind would miss
 * exactly the links the memo's funnels need.
 *
 * No sendBeacon of our own: count.js already sends with navigator.sendBeacon,
 * so a count survives the same-tab navigation that follows the click.
 */

/** What a click on this link should be counted as, or null for nothing. */
export function eventFor(
  link: { getAttribute(name: string): string | null },
  pageHost: string,
): string | null {
  // A link that names its own event wins. The alternative is a list of
  // selectors in here, which drifts the moment someone moves a button -- and
  // drifts silently.
  const named = link.getAttribute('data-event');
  if (named) return named;

  // Everything else that leaves the site, by host, so the long tail is visible
  // without naming each link: ext:github.com, ext:t.me.
  const href = link.getAttribute('href') ?? '';
  if (!/^https?:/i.test(href)) return null;

  let host: string;
  try {
    host = new URL(href).hostname;
  } catch {
    return null;
  }
  return host === pageHost ? null : `ext:${host}`;
}

type Counter = (name: string) => void;

/** GoatCounter treats an event as a page view whose path is the event name. */
const goatcounter: Counter = (name) => {
  // Optional: count.js is fetched from the network, and a blocked request must
  // not turn into an exception on every click.
  (window as { goatcounter?: { count?: (v: object) => void } }).goatcounter?.count?.({
    path: name,
    event: true,
  });
};

export function initEvents(doc: Pick<Document, 'addEventListener'>, pageHost: string, count: Counter = goatcounter) {
  doc.addEventListener('click', (ev) => {
    const target = ev.target as Element | null;
    const link = target?.closest?.('a[href]');
    if (!link) return;
    const name = eventFor(link, pageHost);
    if (name) count(name);
  });
}

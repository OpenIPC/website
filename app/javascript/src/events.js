// Count the clicks that leave the site (#183).
//
// Two percent of visitors ever reach /business or /donate, and whether any of
// them then clicked was unknowable: the business page ends in a mailto:, the
// donate page in a link to another host, and the community page in Telegram
// invites. None of those touch the server, so the access log cannot see them
// and Open Collective's income -- about $15k a year, flat to declining --
// could not be attributed to the site at all.
//
// Delegated from `document`, bound once. Turbo replaces the body on every
// navigation but never the document, so a delegated listener survives where a
// per-page walk would have to be re-run, and re-running a bind is how you end
// up counting one click twice.
//
// The beacon completes despite the navigation because external-links.js gives
// every http(s) link target="_blank": the tab the visitor leaves behind is the
// one that sends the count. A mailto: hands off to the mail client and does
// not navigate at all. Neither case needs sendBeacon, and using it would mean
// a second code path that only runs when something goes wrong.
import { countEvent } from './analytics'

// A link that names its own event wins. The alternative is a list of selectors
// in here, which drifts the moment someone moves a button -- and drifts
// silently, because a missing event looks exactly like nobody clicking.
const NAMED = 'data-event'

export default function initEvents() {
  document.addEventListener('click', ev => {
    const link = ev.target.closest('a[href]')
    if (!link) return

    const named = link.getAttribute(NAMED)
    if (named) return countEvent(named)

    // Everything else that leaves the site, by host, so the long tail is
    // visible without naming each one: ext:github.com, ext:t.me.
    const href = link.getAttribute('href') || ''
    if (!/^https?:/i.test(href)) return

    const url = new URL(href, window.location.origin)
    if (url.hostname === window.location.hostname) return

    countEvent(`ext:${url.hostname}`)
  })
}

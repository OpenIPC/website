/**
 * The navbar's ★ for a signed-in member, and the menu it opens: their page,
 * the leaderboard, a report, a maintainer's queues, and signing out.
 *
 * The chip carries the stars only, the name is the menu's header: the name
 * was the widest thing in the bar and wrapped it to two rows in Russian. A
 * browser that never signed in asks nothing and shows nothing; one that did
 * (the flag lib/club.ts keeps) asks /api/v1/club/me once per page, and a
 * sign-in or sign-out on the page itself updates it at once.
 *
 * The toggle is a `.site-caret` like the bar's own, so SiteHeader's script --
 * which listens on the whole bar, because this renders after it ran -- opens,
 * closes and walks it with the same keys.
 */
import { useEffect, useState } from 'preact/hooks';
import { pathFor, type Locale } from '../../lib/i18n';
import { CHANGED, fetchMe, remembered, signOut, type Member } from '../../lib/club';
import { CLUB_MENU } from '../../lib/nav';
import { Stars } from './parts';

/** nav.* strings, resolved at build time so the island carries no catalogue. */
export type ClubMenuLabels = Record<string, string>;

/** After signing out: start the page over, so no island keeps what it loaded for the member. */
const reload = () => window.location.reload();

export default function ClubMenu({ locale, labels, leave = reload }: { locale: Locale; labels: ClubMenuLabels; leave?: () => void }) {
  const [member, setMember] = useState<Member | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    // The page may sign in or out while open (/club, the Telegram sheet):
    // whatever learns who is signed in announces it, and the chip follows.
    const follow = (e: Event) => setMember((e as CustomEvent<Member | null>).detail);
    window.addEventListener(CHANGED, follow);
    if (remembered()) fetchMe().catch(() => {});
    return () => window.removeEventListener(CHANGED, follow);
  }, []);
  if (!member) return null;

  const entries = CLUB_MENU.filter((e) => !e.maintainer || member.maintainer);
  // The page under the menu may be /club, the review queue or crash triage,
  // with the member's reports, dumps or maintainer controls already on it, or a
  // report form that decided at load which receipt to give. Hiding the chip
  // would leave all of that; a reload is the one way every island starts again
  // as a guest. A logout that failed leaves the member signed in, and says so.
  const out = async () => {
    setFailed(false);
    try {
      await signOut();
    } catch (err) {
      console.error('club: sign-out failed', err);
      setFailed(true);
      return;
    }
    leave();
  };

  return (
    <>
      <a
        class="site-nav-link site-caret"
        href={pathFor(locale, '/club')}
        role="button"
        aria-expanded="false"
        aria-haspopup="true"
        aria-label={`${labels.member_menu}: ${member.name}, ★ ${member.stars}`}
        title={member.name}
      >
        <Stars n={member.stars} onDark />
      </a>
      <ul class="site-dropdown site-dropdown-end">
        <li><h6 class="site-dropdown-header max-w-[16rem] truncate">{member.name}</h6></li>
        {entries.map((e) => (
          <li key={e.key}>
            <a class="site-dropdown-item" href={pathFor(locale, e.path)}>
              {labels[e.key]}
              {e.pending && member.pending > 0 && (
                <span class="ms-2 rounded-full bg-accent px-1.5 text-xs font-semibold text-ink tabular-nums">{member.pending}</span>
              )}
            </a>
          </li>
        ))}
        <li><hr class="site-divider" /></li>
        <li>
          <a class="site-dropdown-item" href={pathFor(locale, '/club')} onClick={(ev) => { ev.preventDefault(); ev.stopPropagation(); void out(); }}>
            {labels.sign_out}
          </a>
          {failed && <p class="m-0 max-w-[16rem] px-4 py-1 text-sm text-[#ff8f8f]" role="alert">{labels.sign_out_failed}</p>}
        </li>
      </ul>
    </>
  );
}

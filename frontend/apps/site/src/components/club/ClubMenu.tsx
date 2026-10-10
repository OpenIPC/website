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

export default function ClubMenu({ locale, labels }: { locale: Locale; labels: ClubMenuLabels }) {
  const [member, setMember] = useState<Member | null>(null);
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
  const out = async () => {
    try { await signOut(); } catch { /* the chip stays; /club says why */ }
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
          <a class="site-dropdown-item" href={pathFor(locale, '/club')} onClick={(ev) => { ev.preventDefault(); void out(); }}>
            {labels.sign_out}
          </a>
        </li>
      </ul>
    </>
  );
}

/**
 * The navbar's ★ and name, for a signed-in member: a link to their page.
 * A browser that never signed in asks nothing and shows nothing; one that
 * did (the flag lib/club.ts keeps) asks /api/v1/club/me once per page, and
 * a sign-in or sign-out on the page itself updates it at once.
 */
import { useEffect, useState } from 'preact/hooks';
import { pathFor, type Locale } from '../../lib/i18n';
import { CHANGED, fetchMe, remembered, type Member } from '../../lib/club';
import { Stars } from './parts';

export default function ClubBadge({ locale }: { locale: Locale }) {
  const [member, setMember] = useState<Member | null>(null);
  useEffect(() => {
    // The page may sign in or out while open (/club, the Telegram sheet):
    // whatever learns who is signed in announces it, and the badge follows.
    const follow = (e: Event) => setMember((e as CustomEvent<Member | null>).detail);
    window.addEventListener(CHANGED, follow);
    if (remembered()) fetchMe().catch(() => {});
    return () => window.removeEventListener(CHANGED, follow);
  }, []);
  if (!member) return null;
  return (
    <a class="site-nav-link" href={pathFor(locale, '/club')} title={member.name}>
      <Stars n={member.stars} onDark />
      <span class="ms-2 inline-block max-w-[12ch] truncate align-bottom">{member.name}</span>
    </a>
  );
}

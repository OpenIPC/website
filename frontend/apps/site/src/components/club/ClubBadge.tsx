/**
 * The navbar's ★ and name, for a signed-in member: a link to their page.
 * A browser that never signed in asks nothing and shows nothing; one that
 * did (the flag lib/club.ts keeps) asks /api/v1/club/me once per page.
 */
import { useEffect, useState } from 'preact/hooks';
import { pathFor, type Locale } from '../../lib/i18n';
import { fetchMe, remembered, type Member } from '../../lib/club';
import { Stars } from './parts';

export default function ClubBadge({ locale }: { locale: Locale }) {
  const [member, setMember] = useState<Member | null>(null);
  useEffect(() => {
    if (remembered()) fetchMe().then((me) => setMember(me.member)).catch(() => {});
  }, []);
  if (!member) return null;
  return (
    <a class="site-nav-link" href={pathFor(locale, '/club')} title={member.name}>
      <Stars n={member.stars} onDark />
      <span class="ms-2 inline-block max-w-[12ch] truncate align-bottom">{member.name}</span>
    </a>
  );
}

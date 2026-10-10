/**
 * The site's navigation, as data (#160).
 *
 * The bar began as a copy of the Rails navbar, entry for entry. It has since
 * been regrouped (News was the entry that made it wrap): six top-level entries
 * at most, each a kind of page rather than a grab-bag, so the bar fits one row
 * at 1200px in all three languages with a member signed in.
 * src/lib/pages.build.test.ts holds the count and a width budget per locale.
 *
 *   * /majestic-endpoints is deliberately absent. 3f0753c took it out of the
 *     menu when the page stopped being a list to browse; the URL stays for the
 *     inbound links and the menu entry does not come back.
 *   * The Ecosystem dropdown's "Web tools" heading is a nested group, drawn as
 *     a header between two dividers.
 *   * The Club is in Community for everyone; a signed-in member also gets the
 *     star chip and its menu (CLUB_MENU, components/club/ClubMenu.tsx).
 */
import type { MenuItems } from '@openipc/ui';
import { pathFor, useTranslations, type Locale } from './i18n';

/** An address the navigation points at: local paths get a locale prefix. */
function href(locale: Locale, path: string): string {
  return path.startsWith('http') ? path : pathFor(locale, path);
}

export function menuFor(locale: Locale): MenuItems {
  const t = useTranslations(locale);
  const link = (id: string, key: string, path: string) => ({
    id,
    label: t(key),
    type: 'link' as const,
    url: href(locale, path),
  });

  return [
    link('get-started', 'nav.get_started', '/get-started'),
    {
      id: 'hardware',
      label: t('nav.hardware'),
      type: 'parent',
      children: [
        link('supported-hardware', 'nav.supported_hardware', '/supported-hardware'),
        link('boards', 'nav.boards', '/cameras/boards'),
        link('report-camera', 'nav.report_camera', '/cameras/report'),
        link('firmware-explorer', 'nav.firmware_explorer', '/firmware-explorer'),
      ],
    },
    {
      id: 'use-cases',
      label: t('nav.use_cases'),
      type: 'parent',
      children: [
        link('low-latency-fpv', 'nav.low_latency_fpv', '/low-latency'),
        link('teleoperation', 'nav.teleoperation', '/teleoperation'),
        link('edge-ai', 'nav.edge_ai', '/edge-ai'),
      ],
    },
    {
      id: 'ecosystem',
      label: t('nav.ecosystem'),
      type: 'parent',
      children: [
        link('ecosystem-overview', 'nav.ecosystem_overview', '/ecosystem'),
        link('web-interface', 'nav.webui', '/web-interface'),
        {
          id: 'web-tools',
          label: t('nav.header_web_tools'),
          type: 'parent',
          children: [
            link('hires-timer', 'nav.hires_timer', '/tools/high-resolution-timer'),
            link('qr-code', 'nav.qr_code_generator', '/tools/qr-code-generator'),
            link('utilities', 'nav.utilities', '/utilities'),
          ],
        },
        link('wiki', 'nav.wiki', 'https://github.com/OpenIPC/wiki'),
      ],
    },
    {
      id: 'business',
      label: t('nav.business'),
      type: 'parent',
      children: [
        link('business-overview', 'nav.business_overview', '/business'),
        link('video-encoding', 'nav.video_encoding', '/video-encoding'),
        link('isp-sensors', 'nav.isp_sensors', '/isp-sensors'),
        link('reverse-engineering', 'nav.reverse_engineering', '/reverse-engineering'),
        link('turnkey-hardware', 'nav.turnkey_hardware', '/turnkey-hardware'),
        link('digital-twins', 'nav.digital_twins', '/digital-twins'),
      ],
    },
    {
      id: 'community',
      label: t('nav.community'),
      type: 'parent',
      children: [
        link('news', 'nav.news', '/news'),
        link('community-chat', 'nav.community_chat', '/community'),
        link('open-wall', 'nav.openwall', '/open-wall'),
        link('club', 'nav.club', '/club'),
        link('team', 'nav.team', '/our-team'),
        link('donate', 'nav.donate', '/donate'),
        link('green-life', 'nav.green_life', '/green_life'),
      ],
    },
    { id: 'github', label: 'GitHub', type: 'link', url: 'https://github.com/OpenIPC' },
  ];
}

/**
 * The signed-in member's menu, under the star chip. Keys are nav.*; the
 * maintainer's entries show only to a member of CLUB_MAINTAINER_ORG, and the
 * review queue carries the count of reports waiting.
 */
export interface ClubMenuEntry {
  key: string;
  path: string;
  maintainer?: boolean;
  pending?: boolean;
}

export const CLUB_MENU: ClubMenuEntry[] = [
  { key: 'my_club', path: '/club' },
  { key: 'leaderboard', path: '/club/leaderboard' },
  { key: 'send_report', path: '/cameras/report' },
  { key: 'review_queue', path: '/club/review', maintainer: true, pending: true },
  { key: 'crash_triage', path: '/club/crashes', maintainer: true },
];

/**
 * Entries that stand for a section rather than one page, and the addresses
 * under them: a news post, the explorer's upstream view, a vendor or a chip of
 * the SoC catalogue. Everything else is current only at its own address -- the
 * Club's leaderboard, review queue and triage are pages of their own, not the
 * Club page.
 */
const SECTIONS: Record<string, string[]> = {
  '/news': ['/news'],
  '/firmware-explorer': ['/firmware-explorer'],
  '/supported-hardware': ['/supported-hardware', '/cameras/vendors'],
};

/** Whether a menu entry is the page being built, or the section it is in. Both are locale-free paths. */
export function isCurrent(url: string | undefined, locale: Locale, path: string): boolean {
  if (!url || url.startsWith('http')) return false;
  const own = url.slice(pathFor(locale, '/').replace(/\/$/, '').length) || '/';
  return path === own || (SECTIONS[own] ?? []).some((base) => path.startsWith(`${base}/`));
}

export interface FooterLink {
  label: string;
  url: string;
}

export interface FooterColumn {
  id: string;
  title: string;
  links: FooterLink[];
}

/**
 * The four footer columns, the bar's groups folded into four.
 *
 * Two links from the draft of this footer are absent for reasons that postdate
 * it and still hold: /binaries answers 410 since 2cf8bc9, and linking a retired
 * URL from every page would be a curious way to retire it; /majestic-endpoints
 * left the menu in 3f0753c and the site does not advertise it.
 */
export function footerFor(locale: Locale): FooterColumn[] {
  const t = useTranslations(locale);
  const link = (key: string, path: string): FooterLink => ({
    label: t(key),
    url: href(locale, path),
  });

  return [
    {
      id: 'platform',
      title: t('footer.column_platform'),
      links: [
        link('nav.get_started', '/get-started'),
        link('nav.supported_hardware', '/supported-hardware'),
        link('nav.boards', '/cameras/boards'),
        link('nav.report_camera', '/cameras/report'),
        link('nav.firmware_explorer', '/firmware-explorer'),
        link('footer.firmware_source', 'https://github.com/OpenIPC/firmware'),
        link('nav.webui', '/web-interface'),
      ],
    },
    {
      id: 'solutions',
      title: t('footer.column_solutions'),
      links: [
        link('nav.low_latency_fpv', '/low-latency'),
        link('nav.teleoperation', '/teleoperation'),
        link('nav.edge_ai', '/edge-ai'),
        link('nav.business', '/business'),
        link('nav.video_encoding', '/video-encoding'),
        link('nav.isp_sensors', '/isp-sensors'),
        link('nav.reverse_engineering', '/reverse-engineering'),
        link('nav.turnkey_hardware', '/turnkey-hardware'),
        link('nav.digital_twins', '/digital-twins'),
      ],
    },
    {
      id: 'project',
      title: t('footer.column_project'),
      links: [
        link('nav.ecosystem_overview', '/ecosystem'),
        link('nav.utilities', '/utilities'),
        link('nav.wiki', 'https://github.com/OpenIPC/wiki'),
        link('nav.team', '/our-team'),
        link('nav.donate', '/donate'),
        link('nav.green_life', '/green_life'),
        link('nav.privacy', '/privacy'),
      ],
    },
    {
      id: 'community',
      title: t('footer.column_community'),
      links: [
        link('nav.news', '/news'),
        link('nav.community_chat', '/community'),
        link('nav.openwall', '/open-wall'),
        link('nav.club', '/club'),
        link('nav.leaderboard', '/club/leaderboard'),
        link('nav.crashes', '/crashes'),
      ],
    },
  ];
}

/**
 * The social row under the community column.
 *
 * Seven, not @openipc/ui's four: the footer carries Telegram, Instagram and
 * Bluesky as well, and Telegram is where the project actually answers
 * questions.
 */
export const SOCIAL_LINKS: { title: string; url: string; icon: string }[] = [
  { title: 'OpenIPC on GitHub', url: 'https://github.com/OpenIPC', icon: 'github' },
  { title: 'OpenIPC on OpenCollective', url: 'https://opencollective.com/openipc', icon: 'opencollective' },
  { title: 'OpenIPC on Telegram', url: 'https://t.me/openipc', icon: 'telegram' },
  { title: 'OpenIPC on YouTube', url: 'https://www.youtube.com/@openipc', icon: 'youtube' },
  { title: 'OpenIPC on Twitter', url: 'https://twitter.com/openipc', icon: 'twitter' },
  { title: 'OpenIPC on Bluesky', url: 'https://bsky.app/profile/openipc.org', icon: 'bluesky' },
  { title: 'OpenIPC on Instagram', url: 'https://www.instagram.com/openipc/', icon: 'instagram' },
];

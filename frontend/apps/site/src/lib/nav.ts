/**
 * The site's navigation, as data (#160).
 *
 * The site's navbar and footer are the source this
 * reproduces, entry for entry, because while the seam is open a visitor can
 * cross between pages and must not see the navigation
 * change shape. When one of those files changes, this changes with it --
 * src/lib/nav.test.ts pins the set of addresses so the two cannot drift
 * quietly.
 *
 * Two things the original navbar does that are worth keeping in mind here:
 *
 *   * /majestic-endpoints is deliberately absent. 3f0753c took it out of the
 *     menu when the page stopped being a list to browse; the URL stays for the
 *     inbound links and the menu entry does not come back.
 *   * The Ecosystem dropdown's "Web tools" heading is a Bootstrap
 *     `dropdown-header` between two dividers. @openipc/ui's menu has no such
 *     ornament, and a nested group says the same thing with structure instead
 *     of with a horizontal rule.
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
    link('hardware', 'nav.hardware', '/supported-hardware'),
    {
      id: 'low-latency',
      label: t('nav.low_latency'),
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
        link('open-wall', 'nav.openwall', '/open-wall'),
        link('web-interface', 'nav.webui', '/web-interface'),
        link('stages', 'nav.stages', '/stages-of-firmware-development'),
        link('firmware-explorer', 'nav.firmware_explorer', '/firmware-explorer'),
        {
          id: 'web-tools',
          label: t('nav.header_web_tools'),
          type: 'parent',
          children: [
            link('partition-calc', 'nav.partition_calc', '/tools/firmware-partitions-calculation'),
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
        link('community-chat', 'nav.community_chat', '/community'),
        link('donate', 'nav.donate', '/donate'),
        link('team', 'nav.team', '/our-team'),
        link('green-life', 'nav.green_life', '/green_life'),
      ],
    },
    { id: 'github', label: 'GitHub', type: 'link', url: 'https://github.com/OpenIPC' },
  ];
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
 * The four footer columns.
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
        link('footer.firmware_source', 'https://github.com/OpenIPC/firmware'),
        link('nav.webui', '/web-interface'),
        link('nav.stages', '/stages-of-firmware-development'),
        link('nav.firmware_explorer', '/firmware-explorer'),
      ],
    },
    {
      id: 'ecosystem',
      title: t('footer.column_ecosystem'),
      links: [
        link('nav.ecosystem_overview', '/ecosystem'),
        link('nav.low_latency', '/low-latency'),
        link('nav.teleoperation', '/teleoperation'),
        link('nav.edge_ai', '/edge-ai'),
        link('nav.openwall', '/open-wall'),
        link('nav.utilities', '/utilities'),
        link('nav.wiki', 'https://github.com/OpenIPC/wiki'),
      ],
    },
    {
      id: 'project',
      title: t('footer.column_project'),
      links: [
        link('nav.business', '/business'),
        link('nav.video_encoding', '/video-encoding'),
        link('nav.isp_sensors', '/isp-sensors'),
        link('nav.reverse_engineering', '/reverse-engineering'),
        link('nav.turnkey_hardware', '/turnkey-hardware'),
        link('nav.digital_twins', '/digital-twins'),
        link('nav.donate', '/donate'),
        link('nav.team', '/our-team'),
        link('nav.green_life', '/green_life'),
        link('nav.privacy', '/privacy'),
      ],
    },
    {
      id: 'community',
      title: t('footer.column_community'),
      links: [link('nav.community_chat', '/community')],
    },
  ];
}

/**
 * The social row under the community column.
 *
 * Six, not @openipc/ui's four: the footer carries Telegram and Instagram
 * as well, and Telegram is where the project actually answers questions.
 */
export const SOCIAL_LINKS: { title: string; url: string; icon: string }[] = [
  { title: 'OpenIPC on GitHub', url: 'https://github.com/OpenIPC', icon: 'github' },
  { title: 'OpenIPC on OpenCollective', url: 'https://opencollective.com/openipc', icon: 'opencollective' },
  { title: 'OpenIPC on Telegram', url: 'https://t.me/openipc', icon: 'telegram' },
  { title: 'OpenIPC on YouTube', url: 'https://www.youtube.com/@openipc', icon: 'youtube' },
  { title: 'OpenIPC on Twitter', url: 'https://twitter.com/openipc', icon: 'twitter' },
  { title: 'OpenIPC on Instagram', url: 'https://www.instagram.com/openipc/', icon: 'instagram' },
];

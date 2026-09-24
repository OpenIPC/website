/**
 * The partner wall, as data (#160).
 *
 * app/helpers/pages_helper.rb held these as Ruby constants; the pages that
 * render them are leaving Rails, so the lists come with them. The grouping,
 * the order, the commented-out entries and the territory rule are all carried
 * across unchanged -- this is a move, not an edit, and src/lib/partners.test.ts
 * pins the counts so a later edit is a deliberate one.
 *
 * "Partners" was doing too much work as one word. A chip vendor that ships
 * OpenIPC out of the box, a company that installs cameras for a living, a
 * university team and the host of our finances are all on the wall for
 * different reasons, and a page usually wants some of those reasons rather
 * than all of them.
 */
import type { Locale } from './i18n';

export interface Partner {
  name: string;
  url: string;
  /** Basename under src/assets/partners, without the extension. */
  img: string;
}

export type PartnerGroupKey =
  | 'global' | 'manufacturers' | 'integrators' | 'fpv' | 'education' | 'exhibitions' | 'research';

/**
 * Entries commented out here are commented out on the Rails wall too, with the
 * same URLs, so nothing appears or disappears silently. Uncommenting a line is
 * how one comes back.
 *
 * partners/baresip_mini.png exists as an asset but has never been on the wall.
 * Left off rather than introduced by a port.
 */
export const PARTNER_GROUPS: Record<PartnerGroupKey, Partner[]> = {
  // Who hosts our money and our code.
  global: [
    { name: 'Open Source Collective', url: 'https://www.oscollective.org/', img: 'osc_mini' },
    { name: 'GitHub', url: 'https://github.com/', img: 'github_mini' },
  ],
  // Ships hardware with OpenIPC on it.
  manufacturers: [
    { name: 'RunCam', url: 'https://runcam.com/', img: 'runcam_mini' },
    { name: 'CCDCAM', url: 'https://ccdcam.com/', img: 'ccdcam_mini' },
    // Commented out on the Rails wall, so commented out here:
    // { name: 'EMAX', url: 'https://emaxmodel.com/', img: 'emax_mini' },
  ],
  // Builds systems on it for other people. RU_INTEGRATORS is appended to this
  // group for Russian-speaking visitors only -- see logosIn.
  integrators: [
    { name: 'GoodCam', url: 'https://www.goodcam.io/', img: 'goodcam_mini' },
    { name: 'AnyCam', url: 'https://anycam.io/', img: 'anycam_mini' },
    { name: 'Faceter', url: 'https://faceter.cam/', img: 'faceter_mini' },
    { name: 'Really', url: 'https://opencollective.com/really-541ee976', img: 'really_mini' },
  ],
  // The FPV projects we grew up alongside.
  fpv: [
    { name: 'wfb-ng', url: 'https://github.com/svpcom/wfb-ng/', img: 'wfb-ng_mini' },
    { name: 'RubyFPV', url: 'https://rubyfpv.com/', img: 'rubyfpv_mini' },
    { name: 'Mario FPV', url: 'https://www.youtube.com/@mariofpv', img: 'mariofpv_mini' },
  ],
  // Student and university teams flying or teaching on OpenIPC.
  education: [
    { name: 'TUDSaT', url: 'https://www.tudsat.space/', img: 'tudsat_mini' },
    { name: 'WuSpace', url: 'https://wuespace.de/', img: 'wuespace_mini' },
  ],
  // Kept in the system, rendered nowhere. There is no page for trade shows and
  // exhibitions yet; when there is, it asks for 'exhibitions' and both the logo
  // and the link are already here. Deliberately absent from HOME_PARTNER_ROWS
  // and from every partnerGroups call -- a test holds it that way.
  exhibitions: [
    { name: 'Expo Electronica', url: 'https://expoelectronica.ru/en/', img: 'expo-electronica_mini' },
  ],
  // Reverse engineering and silicon research we build on.
  research: [
    { name: 'Linux Chenxing', url: 'https://linux-chenxing.org/', img: 'linuxchenxing_mini' },
  ],
};

/**
 * Integrators are territory-specific: these serve Russia and are shown only to
 * Russian-language visitors. Showing them to everyone was explicitly not
 * wanted, and the reverse -- gating them in CSS with `html:not([lang="ru"])` --
 * never worked, because no logo ever carried the `ru` class the rule selects on.
 */
export const RU_INTEGRATORS: Partner[] = [
  { name: 'SkyCam', url: 'https://skycam.cam/', img: 'skycam_mini' },
  { name: 'Vixand', url: 'https://vixand.ru/', img: 'vixand_mini' },
  { name: 'Improve IT', url: 'https://3it.ru/', img: 'improve_mini' },
  { name: 'UfaNet', url: 'https://www.ufanet.ru/', img: 'ufanet_mini' },
  { name: 'Dvor24', url: 'https://dvor24.ru/', img: 'dvor24_mini' },
  { name: 'Sputnik', url: 'https://sputnik.systems/', img: 'sputnik_mini' },
  { name: 'Techno-Shield', url: 'https://msvoko.ru/', img: 'techno-shield_mini' },
  { name: 'KeyTelecom', url: 'https://keytele.com/', img: 'keytelecom_mini' },
  { name: 'WebGlazok', url: 'https://webglazok.com/', img: 'webglazok_mini' },
  { name: 'Yucca', url: 'https://yucca.app/en', img: 'yucca_mini' },
  { name: 'IPEYE', url: 'https://ipeye.ru/', img: 'ipeye_mini' },
  { name: 'VTL', url: 'https://vtl.su/#rec35109538', img: 'vtl_mini' },
  { name: 'S-Video', url: 'https://www.cctvsp.ru/cctv/openipc', img: 's-video_mini' },
  { name: 'MyWiFi', url: 'https://xn--80aaaf0bh2e7a5c.xn--p1ai/', img: 'mywifi-cc_mini' },
  {
    name: 'AlarmSystem',
    url: 'https://alarmsystem-cctv.ru/product-category/cctv-products/cctv-cameras/ip-cameras-cctv/'
      + '?swoof=1&product_brands=openipc&really_curr_tax=189-product_cat',
    img: 'alarmsystem_mini',
  },
  // Commented out on the Rails wall, so commented out here:
  // { name: 'MegaCam', url: 'https://megacam.kz/', img: 'megacam_mini' },
  // { name: 'Dozor', url: 'https://dozor-smart.ru/', img: 'dozor_mini' },
  // { name: 'Flagman', url: 'https://flagman.org/', img: 'flagman_mini' },
  // { name: 'Meldana', url: 'https://meldana.com/', img: 'meldana_mini' },
  // { name: 'Binary Machines', url: 'https://bmachines.ru/', img: 'binary-machines_mini' },
  // { name: 'GAINS', url: 'https://gains.company/', img: 'gain_mini' },
];

/**
 * How the home page arranges the groups.
 *
 * At six rows half of them were a single logo under a heading, which reads as a
 * gap rather than a group -- research is one logo, and integrators is one until
 * the visitor is Russian. Three rows, in the order the wall is meant to be
 * read: who ships and installs the hardware, who hosts us, and who we work
 * alongside. Order matters twice over: the rows appear in the order written
 * here, and within a row the groups are laid out in the order they are listed.
 */
export const HOME_PARTNER_ROWS: Record<string, PartnerGroupKey[]> = {
  global: ['global'],
  trade: ['manufacturers', 'integrators'],
  friends: ['research', 'fpv', 'education'],
};

/**
 * The Russian integrator list is appended here and nowhere else, so every
 * caller gets the same territory rule whether it asks for a group or a row.
 */
export function logosIn(locale: Locale, key: PartnerGroupKey): Partner[] {
  const logos = PARTNER_GROUPS[key];
  return key === 'integrators' && locale === 'ru' ? [...logos, ...RU_INTEGRATORS] : logos;
}

/**
 * Rows of [label, logos], in the order HOME_PARTNER_ROWS declares. A row whose
 * groups are all empty is dropped rather than rendered as a heading over
 * nothing -- which is what a territory-specific group looks like in a locale
 * that has no entries for it yet.
 */
export function partnerRows(
  locale: Locale,
  rows: Record<string, PartnerGroupKey[]> = HOME_PARTNER_ROWS,
): [string, Partner[]][] {
  return Object.entries(rows)
    .map(([label, keys]) => [label, keys.flatMap((k) => logosIn(locale, k))] as [string, Partner[]])
    .filter(([, logos]) => logos.length > 0);
}

/**
 * The named groups, in the order asked for, each as [key, logos]. Groups that
 * would render empty are dropped rather than left as a heading over nothing.
 * With no keys, every group -- which is why a caller that means "some" must
 * always name them.
 */
export function partnerGroups(
  locale: Locale,
  ...keys: PartnerGroupKey[]
): [PartnerGroupKey, Partner[]][] {
  const wanted = keys.length > 0 ? keys : (Object.keys(PARTNER_GROUPS) as PartnerGroupKey[]);
  return wanted
    .map((key) => [key, logosIn(locale, key)] as [PartnerGroupKey, Partner[]])
    .filter(([, logos]) => logos.length > 0);
}

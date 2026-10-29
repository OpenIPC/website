/**
 * The partner wall, as data (#160).
 *
 * The partner lists the pages render. The grouping, the order and the
 * territory rule were carried across from the original wall; entries have been
 * taken off and groups placed since, each in its own pull request.
 * src/lib/pages.build.test.ts holds the territory rule, which groups /business
 * shows and where exhibitions shows, so an edit that changes any of them is a
 * deliberate one.
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
  /** Locales this logo is not shown in -- a partner that does not operate there. */
  hiddenIn?: Locale[];
  /** Locales this logo is shown in, and only those -- a partner that operates nowhere else. */
  onlyIn?: Locale[];
  /** The link in a locale, where the partner's site has a page in that language and `url` is another. */
  urlIn?: Partial<Record<Locale, string>>;
}

export type PartnerGroupKey =
  | 'global' | 'manufacturers' | 'integrators' | 'fpv' | 'education' | 'exhibitions' | 'research';

/**
 * EMAX and the six at the end of RU_INTEGRATORS were commented out on the
 * original wall and are carried across that way, with the same URLs. Entries
 * taken off since -- Techno-Shield (#438), GoodCam and AnyCam (#528), WebGlazok,
 * Yucca, MyWiFi and AlarmSystem (#532), RunCam and CCDCAM -- say why next to
 * them. Uncommenting a line is how one comes back.
 *
 * partners/majestic_mini.png is on no list and is kept on purpose.
 */
export const PARTNER_GROUPS: Record<PartnerGroupKey, Partner[]> = {
  // Who hosts our money and our code.
  global: [
    { name: 'Open Source Collective', url: 'https://www.oscollective.org/', img: 'osc_mini' },
    { name: 'GitHub', url: 'https://github.com/', img: 'github_mini' },
  ],
  // Ships hardware with OpenIPC on it.
  manufacturers: [
    // Cooperation suspended; off the wall, uncomment to bring it back.
    // { name: 'RunCam', url: 'https://runcam.com/', img: 'runcam_mini' },
    { name: 'AnjVision', url: 'https://www2.anjvision.com/', img: 'anjvision_mini' },
    // Cooperation suspended; off the wall, uncomment to bring it back.
    // { name: 'CCDCAM', url: 'https://ccdcam.com/', img: 'ccdcam_mini' },
    { name: 'IR LAB', url: 'https://irlab.net/', img: 'irlab_mini' },
    // SkyCam serves Russia only.
    { name: 'SkyCam', url: 'https://openipc.su/', img: 'skycam_mini', onlyIn: ['ru'] },
    // Commented out on the original wall, so commented out here:
    // { name: 'EMAX', url: 'https://emaxmodel.com/', img: 'emax_mini' },
  ],
  // Builds systems on it for other people. RU_INTEGRATORS is appended to this
  // group for Russian-speaking visitors only -- see logosIn.
  integrators: [
    { name: 'Faceter', url: 'https://faceter.cam/', img: 'faceter_mini' },
    // GoodCam and AnyCam do not operate in Russia. Off the wall on every page
    // for now; uncomment to bring them back.
    // { name: 'GoodCam', url: 'https://www.goodcam.io/', img: 'goodcam_mini', hiddenIn: ['ru'] },
    // { name: 'AnyCam', url: 'https://anycam.io/', img: 'anycam_mini', hiddenIn: ['ru'] },
  ],
  // The FPV projects we grew up alongside.
  fpv: [
    { name: 'wfb-ng', url: 'https://github.com/svpcom/wfb-ng/', img: 'wfb-ng_mini' },
    { name: 'RubyFPV', url: 'https://rubyfpv.com/', img: 'rubyfpv_mini' },
    { name: 'Mario FPV', url: 'https://www.youtube.com/@mariofpv', img: 'mariofpv_mini' },
    { name: 'Really', url: 'https://opencollective.com/really-541ee976', img: 'really_mini' },
  ],
  // Student and university teams flying or teaching on OpenIPC.
  education: [
    { name: 'TUDSaT', url: 'https://www.tudsat.space/', img: 'tudsat_mini' },
    { name: 'WuSpace', url: 'https://wuespace.de/', img: 'wuespace_mini' },
  ],
  // Trade shows and exhibitions. Rendered nowhere for now: one logo is a gap
  // rather than a group. Two places are ready for it, commented out -- the
  // home page's friends row (HOME_PARTNER_ROWS) and a wall of its own on
  // /business (Business.astro) -- and come in once there are two or three
  // shows. A test holds it hidden until then. Expo Electronica is for Russian-
  // and Chinese-speaking visitors only; its site has no Chinese version, so
  // /zh/ links the English one.
  exhibitions: [
    {
      name: 'Expo Electronica', url: 'https://expoelectronica.ru/', img: 'expo-electronica_mini',
      onlyIn: ['ru', 'zh'], urlIn: { zh: 'https://expoelectronica.ru/en/' },
    },
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
  { name: 'Vixand', url: 'https://vixand.ru/', img: 'vixand_mini' },
  { name: 'Improve IT', url: 'https://3it.ru/', img: 'improve_mini' },
  { name: 'UfaNet', url: 'https://www.ufanet.ru/', img: 'ufanet_mini' },
  { name: 'Dvor24', url: 'https://dvor24.ru/', img: 'dvor24_mini' },
  { name: 'Sputnik', url: 'https://sputnik.systems/', img: 'sputnik_mini' },
  // Temporarily off the wall; uncomment to bring it back.
  // { name: 'Techno-Shield', url: 'https://msvoko.ru/', img: 'techno-shield_mini' },
  { name: 'KeyTelecom', url: 'https://keytele.com/', img: 'keytelecom_mini' },
  // No longer current; off the wall, uncomment to bring it back.
  // { name: 'WebGlazok', url: 'https://webglazok.com/', img: 'webglazok_mini' },
  // { name: 'Yucca', url: 'https://yucca.app/en', img: 'yucca_mini' },
  { name: 'IPEYE', url: 'https://ipeye.ru/', img: 'ipeye_mini' },
  { name: 'VTL', url: 'https://vtl.su/#rec35109538', img: 'vtl_mini' },
  { name: 'S-Video', url: 'https://www.cctvsp.ru/cctv/openipc', img: 's-video_mini' },
  // No longer current; off the wall, uncomment to bring them back.
  // { name: 'MyWiFi', url: 'https://xn--80aaaf0bh2e7a5c.xn--p1ai/', img: 'mywifi-cc_mini' },
  // {
  //   name: 'AlarmSystem',
  //   url: 'https://alarmsystem-cctv.ru/product-category/cctv-products/cctv-cameras/ip-cameras-cctv/'
  //     + '?swoof=1&product_brands=openipc&really_curr_tax=189-product_cat',
  //   img: 'alarmsystem_mini',
  // },
  // Commented out on the original wall, so commented out here:
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
 * read: who hosts us, who ships and installs the hardware, and who we work
 * alongside. Order matters twice over: the rows appear in the order written
 * here, and within a row the groups are laid out in the order they are listed.
 */
export const HOME_PARTNER_ROWS: Record<string, PartnerGroupKey[]> = {
  global: ['global'],
  trade: ['manufacturers', 'integrators'],
  // Exhibitions join the end of this row once there are two or three -- see
  // PARTNER_GROUPS.exhibitions.
  friends: ['research', 'fpv', 'education' /* , 'exhibitions' */],
};

/**
 * The territory rules live here and nowhere else -- the Russian integrator list
 * appended, logos marked hiddenIn this locale dropped, logos marked onlyIn
 * other locales dropped, and the link swapped for the locale's own (urlIn) --
 * so every caller gets the same answer whether it asks for a group or a row.
 */
export function logosIn(locale: Locale, key: PartnerGroupKey): Partner[] {
  const logos = PARTNER_GROUPS[key]
    .filter((p) => !p.hiddenIn?.includes(locale) && (!p.onlyIn || p.onlyIn.includes(locale)))
    .map((p) => (p.urlIn?.[locale] ? { ...p, url: p.urlIn[locale] } : p));
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

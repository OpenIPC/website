/**
 * Storybook fixture. NOT the partner list.
 *
 * The <Supporters> widget takes logo URLs as props precisely so that the
 * package ships no partner artwork: on openipc.org those files live in
 * app/assets/images/partners/ and the list itself is PARTNER_GROUPS in
 * the site's partner list, which #160 turned into a data file. These four
 * point at the projects' own public marks so the story has something to draw.
 */
import type { Supporter } from '../components/widgets/supporters';

export const SUPPORTERS: Supporter[] = [
  {
    name: 'Open Source Collective',
    href: 'https://www.oscollective.org/',
    logoUrl: 'https://images.opencollective.com/opensource/426badd/logo/96.png',
  },
  {
    name: 'GitHub',
    href: 'https://github.com/',
    logoUrl: 'https://github.githubassets.com/favicons/favicon.svg',
  },
  {
    name: 'linux-chenxing',
    href: 'https://linux-chenxing.org/',
    logoUrl: 'https://avatars.githubusercontent.com/u/79362408',
  },
  {
    name: 'wfb-ng',
    href: 'https://github.com/svpcom/wfb-ng/',
    logoUrl: 'https://avatars.githubusercontent.com/u/1296596',
  },
];

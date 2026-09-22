/**
 * Storybook fixture. NOT the roster.
 *
 * Six of the thirty-six people openipc.org/our-team lists today, copied from
 * app/views/pages/our_team.html.erb -- which is the roster of record until
 * #160 turns that template into a data file. The package carries a handful
 * rather than all of them on purpose: a component library holding a second
 * copy of the team is a second copy to go stale.
 */
import type { TeamProps } from '../components/widgets/team/team-types';

export const TEAM: TeamProps['members'] = [
  {
    imgSrc: 'https://avatars.githubusercontent.com/u/46071473',
    name: 'OpenIPC',
    bio: 'Official community account on all platforms and sites',
    socials: [
      { link: 'https://github.com/OpenIPC/', icon: 'Github' },
      { link: 'https://opencollective.com/openipc', icon: 'OpenCollective' },
      { link: 'https://t.me/OpenIPC', icon: 'Telegram' },
      { link: 'https://youtube.com/@openipc', icon: 'YouTube' },
    ],
  },
  {
    imgSrc: 'https://avatars.githubusercontent.com/u/68112357',
    name: 'FlyRouter',
    bio: 'Project Coordination, Documentation, Builder, Composer',
    socials: [
      { link: 'https://github.com/FlyRouter/', icon: 'Github' },
      { link: 'https://t.me/FlyRouter', icon: 'Telegram' },
    ],
  },
  {
    imgSrc: 'https://avatars.githubusercontent.com/u/6576495',
    name: 'widgetii',
    bio: 'Majestic Streamer, IPCtool, Linux',
    socials: [
      { link: 'https://github.com/widgetii/', icon: 'Github' },
      { link: 'https://t.me/widgetii', icon: 'Telegram' },
    ],
  },
  {
    imgSrc: 'https://avatars.githubusercontent.com/u/7954832',
    name: 'Dimerr',
    bio: 'U-Boot, Kernels, Coupler, IPCtool',
    socials: [
      { link: 'https://github.com/dimerr/', icon: 'Github' },
      { link: 'https://t.me/dimerrr', icon: 'Telegram' },
    ],
  },
  {
    imgSrc: 'https://avatars.githubusercontent.com/u/40539574',
    name: 'Hirrolot',
    bio: 'SmolRTSP',
    socials: [
      { link: 'https://github.com/Hirrolot/', icon: 'Github' },
      { link: 'https://t.me/hirrolot', icon: 'Telegram' },
    ],
  },
  {
    imgSrc: 'https://avatars.githubusercontent.com/u/2557102',
    name: 'cronyx',
    bio: 'Linux, Builder, Composer',
    socials: [
      { link: 'https://github.com/cronyx/', icon: 'Github' },
      { link: 'https://t.me/cronyx', icon: 'Telegram' },
    ],
  },
];

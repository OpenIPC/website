/**
 * The people on /our-team, as data (#160).
 *
 * The team page once held 36 cards of hand-written HTML, each
 * repeating the same markup around a name, an avatar, a role and a handful of
 * links. Extracted mechanically from that file rather than retyped -- 36 cards
 * and 75 links is exactly the volume at which a transcription error goes
 * unnoticed -- and checked against what production serves: the same 36 cards
 * and the same 75 links.
 *
 * The avatars are GitHub's, loaded from avatars.githubusercontent.com, as they
 * were on the original page. That is a third-party request from the visitor's
 * browser and it is worth knowing about; it is not a change this page makes,
 * and baking them into the bundle would mean 36 faces going stale whenever
 * somebody changes their picture.
 */

export interface TeamSocial {
  url: string;
  /** A basename under src/assets/social. */
  icon: string;
  handle: string;
}

export interface TeamMember {
  name: string;
  avatar: string;
  role: string;
  socials: TeamSocial[];
}

export interface TeamSection {
  title: string;
  members: TeamMember[];
}

export const TEAM: TeamSection[] = [
  {
    title: 'IPC Core',
    members: [
      {
        name: 'OpenIPC',
        avatar: 'https://avatars.githubusercontent.com/u/46071473',
        role: 'Official community account on all platforms and sites',
        socials: [{ url: 'https://github.com/OpenIPC/', icon: 'github', handle: '@OpenIPC' }, { url: 'https://opencollective.com/openipc', icon: 'opencollective', handle: '@OpenIPC' }, { url: 'https://t.me/OpenIPC', icon: 'telegram', handle: '@OpenIPC' }, { url: 'http://youtube.com/@openipc', icon: 'youtube', handle: '@OpenIPC' }, { url: 'http://twitter.com/openipc', icon: 'twitter', handle: '@OpenIPC' }],
      },
      {
        name: 'FlyRouter',
        avatar: 'https://avatars.githubusercontent.com/u/68112357',
        role: 'Project Coordination, Documentation, Builder, Composer',
        socials: [{ url: 'https://github.com/FlyRouter/', icon: 'github', handle: '@FlyRouter' }, { url: 'https://t.me/FlyRouter', icon: 'telegram', handle: '@FlyRouter' }],
      },
      {
        name: 'widgetii',
        avatar: 'https://avatars.githubusercontent.com/u/6576495',
        role: 'Majestic Streamer, IPCtool, Linux',
        socials: [{ url: 'https://github.com/widgetii/', icon: 'github', handle: '@widgetii' }, { url: 'https://t.me/widgetii', icon: 'telegram', handle: '@widgetii' }],
      },
      {
        name: 'Dimerr',
        avatar: 'https://avatars.githubusercontent.com/u/7954832',
        role: 'U-Boot, Kernels, Coupler, IPCtool',
        socials: [{ url: 'https://github.com/dimerr/', icon: 'github', handle: '@dimerrr' }, { url: 'https://t.me/dimerrr', icon: 'telegram', handle: '@dimerrr' }],
      },
      {
        name: 'Hirrolot',
        avatar: 'https://avatars.githubusercontent.com/u/40539574',
        role: 'SmolRTSP',
        socials: [{ url: 'https://github.com/Hirrolot/', icon: 'github', handle: '@Hirrolot' }, { url: 'https://t.me/hirrolot', icon: 'telegram', handle: '@hirrolot' }],
      },
      {
        name: 'cronyx',
        avatar: 'https://avatars.githubusercontent.com/u/2557102',
        role: 'Linux, Builder, Composer',
        socials: [{ url: 'https://github.com/cronyx/', icon: 'github', handle: '@cronyx' }, { url: 'https://t.me/cronyx', icon: 'telegram', handle: '@cronyx' }],
      },
      {
        name: 'Ystinia',
        avatar: 'https://avatars.githubusercontent.com/u/94921687',
        role: 'OpenCollective manager, Educational Directions in Europe, Documentation',
        socials: [{ url: 'https://github.com/Ystinia/', icon: 'github', handle: '@Ystinia' }, { url: 'https://t.me/Ystinia5', icon: 'telegram', handle: '@Ystinia5' }],
      },
      {
        name: 'Alex',
        avatar: 'https://avatars.githubusercontent.com/u/131543271',
        role: 'Research Directions in Europe, Documentation',
        socials: [{ url: 'https://github.com/ialexlog/', icon: 'github', handle: '@ialexlog' }, { url: 'https://t.me/metsys1', icon: 'telegram', handle: '@metsys1' }],
      },
      {
        name: 'viktorxda',
        avatar: 'https://avatars.githubusercontent.com/u/35473052',
        role: 'Experiments and development based on SigmaStar processors',
        socials: [{ url: 'https://github.com/viktorxda/', icon: 'github', handle: '@viktorxda' }, { url: 'https://t.me/viktorxda', icon: 'telegram', handle: '@viktorxda' }],
      },
      {
        name: 'Maxim',
        avatar: 'https://avatars.githubusercontent.com/u/5235386',
        role: 'Hardware Development Engineer',
        socials: [{ url: 'https://github.com/fmg-magnus/', icon: 'github', handle: '@fmg-magnus' }, { url: 'https://t.me/GMA_ipc', icon: 'telegram', handle: '@GMA_ipc' }],
      },
      {
        name: 'keyldev',
        avatar: 'https://avatars.githubusercontent.com/u/55500552',
        role: 'Software Engineer, Android and .NET Application Developer',
        socials: [{ url: 'https://github.com/keyldev/', icon: 'github', handle: '@keyldev' }, { url: 'https://t.me/keyldev', icon: 'telegram', handle: '@keyldev' }],
      },
      {
        name: 'JimSmith',
        avatar: 'https://avatars.githubusercontent.com/u/47976944',
        role: 'Linux, Builder',
        socials: [{ url: 'https://github.com/jimsmt/', icon: 'github', handle: '@jimsmt' }, { url: 'https://t.me/@jsmth99', icon: 'telegram', handle: '@jsmth99' }],
      },
      {
        name: 'wberube',
        avatar: 'https://avatars.githubusercontent.com/u/8108928',
        role: 'Divinus Streamer, utilities and libraries for OSD',
        socials: [{ url: 'https://github.com/wberube/', icon: 'github', handle: '@wberube' }, { url: 'https://t.me/wberube', icon: 'telegram', handle: '@wberube' }],
      },
      {
        name: 'skilurius',
        avatar: 'https://avatars.githubusercontent.com/u/26750683',
        role: 'Linux, Firmware, WebUI and users support',
        socials: [{ url: 'https://github.com/skilurius/', icon: 'github', handle: '@skilurius' }, { url: 'https://t.me/skilurius', icon: 'telegram', handle: '@skilurius' }],
      },
      {
        name: 'yarobash',
        avatar: 'https://avatars.githubusercontent.com/u/31137747',
        role: 'Development of WebUI multifunctional interface fancyweb-ng',
        socials: [{ url: 'https://github.com/yarobash/', icon: 'github', handle: '@yarobash' }, { url: 'https://t.me/LaikaPanda', icon: 'telegram', handle: '@LaikaPanda' }],
      },
      {
        name: 'John',
        avatar: 'https://avatars.githubusercontent.com/u/21039610',
        role: 'Linux, Coupler, Builder',
        socials: [{ url: 'https://github.com/p0i5k/', icon: 'github', handle: '@p0i5k' }, { url: 'https://t.me/p0i5k', icon: 'telegram', handle: '@p0i5k' }],
      },
      {
        name: 'Median Trading Ltd.',
        avatar: 'https://avatars.githubusercontent.com/u/49194169',
        role: 'Production and sale of OpenIPC equipment',
        socials: [{ url: 'https://github.com/ser177/', icon: 'github', handle: '@ser177' }, { url: 'https://t.me/ser177', icon: 'telegram', handle: '@ser177' }],
      },
      {
        name: 'Vixand',
        avatar: 'https://avatars.githubusercontent.com/u/183216891',
        role: 'Distribution and promotion IPCam',
        socials: [{ url: 'https://github.com/vixand/', icon: 'github', handle: '@vixand' }, { url: 'https://t.me/ktotud', icon: 'telegram', handle: '@ktotud' }],
      },
      {
        name: 'chertov',
        avatar: 'https://avatars.githubusercontent.com/u/436625',
        role: 'Mini Streamer',
        socials: [{ url: 'https://github.com/chertov/', icon: 'github', handle: '@chertov' }, { url: 'https://t.me/mAX3773', icon: 'telegram', handle: '@mAX3773' }],
      },
      {
        name: 'SSharshunov',
        avatar: 'https://avatars.githubusercontent.com/u/2909755',
        role: 'The ex researcher',
        socials: [{ url: 'https://github.com/SSharshunov/', icon: 'github', handle: '@SSharshunov' }, { url: 'https://t.me/USSSSSH', icon: 'telegram', handle: '@USSSSSH' }],
      },
      {
        name: 'themactep',
        avatar: 'https://avatars.githubusercontent.com/u/37488',
        role: 'Web UI, Documentation',
        socials: [{ url: 'https://github.com/themactep/', icon: 'github', handle: '@themactep' }, { url: 'https://t.me/themactep', icon: 'telegram', handle: '@themactep' }],
      },
    ],
  },
  {
    title: 'URLLC and FPV',
    members: [
      {
        name: 'iHardRock',
        avatar: 'https://avatars.githubusercontent.com/u/8301265',
        role: 'Venc, Vdec, FPV systems',
        socials: [{ url: 'https://github.com/iHardRock/', icon: 'github', handle: '@iHardRock' }, { url: 'https://t.me/getdataflow', icon: 'telegram', handle: '@getdataflow' }],
      },
      {
        name: 'MarioFPV',
        avatar: 'https://avatars.githubusercontent.com/u/57532232',
        role: 'Media Partner, Contributor for configurator and vdec',
        socials: [{ url: 'https://github.com/MarioFPVdev/', icon: 'github', handle: '@MarioFPVdev' }, { url: 'https://t.me/Mario_FPV', icon: 'telegram', handle: '@Mario_FPV' }],
      },
      {
        name: 'KennyPlus',
        avatar: 'https://avatars.githubusercontent.com/u/148837522',
        role: 'Hardware Development Engineer, FPV Systems',
        socials: [{ url: 'https://github.com/KennyPlus/', icon: 'github', handle: '@KennyPlus' }, { url: 'https://t.me/@kenny_plus', icon: 'telegram', handle: '@kenny_plus' }],
      },
      {
        name: 'TipoMan',
        avatar: 'https://avatars.githubusercontent.com/u/97629810',
        role: 'Linux, Sensor Drivers, FPV Systems',
        socials: [{ url: 'https://github.com/tipoman9/', icon: 'github', handle: '@tipoman9' }, { url: 'https://t.me/@tipoman', icon: 'telegram', handle: '@tipoman' }],
      },
      {
        name: 'Milos',
        avatar: 'https://avatars.githubusercontent.com/u/161963520',
        role: 'Linux, Sensor Drivers, FPV Systems',
        socials: [{ url: 'https://github.com/Sakalva/', icon: 'github', handle: '@Sakalva' }, { url: 'https://t.me/@Sakalva', icon: 'telegram', handle: '@Sakalva' }],
      },
      {
        name: 'Ihor',
        avatar: 'https://avatars.githubusercontent.com/u/6557185',
        role: 'Contributor for pixelpilot, devourer, wfb-ng, adaptive-link, docs, builder, firmware and other repos',
        socials: [{ url: 'https://github.com/vertexodessa/', icon: 'github', handle: '@vertexodessa' }, { url: 'https://t.me/@i_am_ihor', icon: 'telegram', handle: '@i_am_ihor' }],
      },
      {
        name: 'ViperZ28',
        avatar: 'https://avatars.githubusercontent.com/u/1970342',
        role: 'Contributor for openipc-configurator, fpv-presets, majestic-webui, wiki firmware and other repos',
        socials: [{ url: 'https://github.com/mikecarr/', icon: 'github', handle: '@mikecarr' }, { url: 'https://t.me/@mikecarr', icon: 'telegram', handle: '@mikecarr' }],
      },
      {
        name: 'JohhnGoblin',
        avatar: 'https://avatars.githubusercontent.com/u/35317840',
        role: 'Contributor for sbc-groundstations, radxa_gs_webUI, configurator and other repos',
        socials: [{ url: 'https://github.com/JohnDGodwin/', icon: 'github', handle: '@JohnDGodwin' }, { url: 'https://t.me/@JohhnGoblin', icon: 'telegram', handle: '@JohhnGoblin' }],
      },
      {
        name: 'CC',
        avatar: 'https://avatars.githubusercontent.com/u/10471035',
        role: 'Contributor for sbc-groundstations and other repos',
        socials: [{ url: 'https://github.com/zhouruixi/', icon: 'github', handle: '@zhouruixi' }, { url: 'https://t.me/@zhouruixi', icon: 'telegram', handle: '@zhouruixi' }],
      },
      {
        name: 'Henk',
        avatar: 'https://avatars.githubusercontent.com/u/1641119',
        role: 'Contributor for pixelpilot_rk, adaptive-link, msposd and other repos',
        socials: [{ url: 'https://github.com/henkwiedig/', icon: 'github', handle: '@henkwiedig' }, { url: 'https://t.me/@derHenk1980', icon: 'telegram', handle: '@derHenk1980' }],
      },
      {
        name: 'Eduardo',
        avatar: 'https://avatars.githubusercontent.com/u/33513057',
        role: 'Contributor for wiki and other repos',
        socials: [{ url: 'https://github.com/OneManChop/', icon: 'github', handle: '@OneManChop' }, { url: 'https://t.me/@eddybr85', icon: 'telegram', handle: '@eddybr85' }],
      },
      {
        name: 'Wojciech',
        avatar: 'https://avatars.githubusercontent.com/u/25986626',
        role: 'Contributor for docs, wiki and other repos',
        socials: [{ url: 'https://github.com/wkumik/', icon: 'github', handle: '@wkumik' }, { url: 'https://t.me/@Wojciech_99', icon: 'telegram', handle: '@Wojciech_99' }],
      },
      {
        name: 'Joakim',
        avatar: 'https://avatars.githubusercontent.com/u/16814703',
        role: 'Contributor for steam-groundstations, firmware and other repos',
        socials: [{ url: 'https://github.com/snokvist/', icon: 'github', handle: '@snokvist' }, { url: 'https://t.me/@snokvist', icon: 'telegram', handle: '@snokvist' }],
      },
      {
        name: 'Mike',
        avatar: 'https://avatars.githubusercontent.com/u/181492598',
        role: 'Device tester and contributor to some repos',
        socials: [{ url: 'https://github.com/notsudogood/', icon: 'github', handle: '@notsudogood' }, { url: 'https://t.me/@notsudogood', icon: 'telegram', handle: '@notsudogood' }],
      },
      {
        name: 'Greg',
        avatar: 'https://avatars.githubusercontent.com/u/55742743',
        role: 'Contributor for adaptive-link, air_manager and other repos',
        socials: [{ url: 'https://github.com/sickgreg/', icon: 'github', handle: '@sickgreg' }, { url: 'https://t.me/@sickgregFPV', icon: 'telegram', handle: '@sickgregFPV' }],
      },
    ],
  },
];

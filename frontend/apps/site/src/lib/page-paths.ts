/**
 * Every address the bundle claims (#160).
 *
 * Addresses only, and deliberately no component imports: this list is what the
 * seam is made of, and the things that need it are not all page renderers.
 * `src/lib/site.build.test.ts` reads it to check the built tree, and a vitest
 * run has no Astro plugin -- importing a `.astro` file here would break every
 * consumer that only wanted to know what URLs exist.
 *
 * `path` is the address without a locale prefix, exactly as the site has always
 * writes it -- including `/green_life`, which is the one marketing URL with an
 * underscore in it. It is not a typo and renaming it would break every inbound
 * link.
 *
 * Two kinds of address are absent on purpose:
 *
 *   * `/`, `/ru/` and `/zh/`, which are the home page and have file routes of
 *     their own -- ../pages/index.astro and ../pages/[locale]/index.astro --
 *     because a rest parameter cannot match an empty path. `/` was the last
 *     address the server rendered for a reader; it left in #165, when the language
 *     choice moved into the browser.
 *   * the ~33 redirects, the two `410 Gone` routes and the catch-all, which
 *     are cheap, tested, and reached by falling through the seam.
 */
import { VENDORS } from './hardware';
/**
 * The catalogue's own addresses (#162), derived from the same data the pages
 * render: recommended, one tab per vendor, and the full list. Written out here
 * rather than by hand so a vendor added to data/catalogue cannot be a page
 * nobody routed to.
 */
function hardwarePaths(): PagePath[] {
  return [
    { path: '/supported-hardware/featured', titleKey: 'cameras.socs.index.title' },
    { path: '/supported-hardware/full-list', titleKey: 'cameras.socs.index.title' },
    ...VENDORS.map((vendor) => ({
      path: `/cameras/vendors/${vendor.urlname}`,
      titleKey: 'cameras.socs.index.title',
    })),

    // One wizard per SoC (#164). 126 of them, and the address is the one
    // it always had: a link anyone has shared still opens the page it opened.
    ...VENDORS.flatMap((vendor) => vendor.socs.map((soc) => ({
      path: `/cameras/vendors/${vendor.urlname}/socs/${soc.urlname}`,
      titleKey: 'cameras.socs.show.title',
    }))),
  ];
}

export interface PagePath {
  /** Locale-free address, leading slash, no trailing slash. */
  path: string;
  /** The catalogue key for <title>. */
  titleKey: string;
  /** The catalogue key for the meta description, when the page sets its own. */
  descriptionKey?: string;
  /** Keep it out of search results. The smoke page only. */
  noindex?: boolean;
  /** Served at addresses it cannot know, so it claims none. See Base.astro. */
  addressless?: boolean;
}

export const PAGE_PATHS: PagePath[] = [
  ...hardwarePaths(),

  // The diagnostic. Not a marketing page and never indexed, but it is an
  // address the bundle claims and it belongs in the same list as the rest --
  // deploy/static.sh, check-bundle.sh and check-config.sh all assert on it.
  { path: '/_smoke', titleKey: 'site.default_meta_description', noindex: true },

  // The Open Wall's shell (#165). One file per locale, and nginx serves it for
  // every wall address -- the gallery, a page of it, a camera permalink, a
  // snapshot, its archive and its slideshow. It is here under a name of its
  // own because the addresses it answers carry ids that change
  // by the hour: `/snapshots/<id>` cannot be a file, and a bundle that claimed
  // `/snapshots` would shadow the route cameras POST to.
  //
  // noindex, and that is about the addresses it is served AT rather than about
  // this one. A snapshot lives two days; indexing 3,210 pages that die within
  // 48 hours is crawl budget spent on nothing, which #165 asks to stop. The
  // gallery is a different matter -- it is in the navbar and the footer and
  // should be found -- so it is a page of its own below, rendering the same
  // island at an address that carries a canonical and no robots directive.
  { path: '/_shell/wall', titleKey: 'title.openwall', noindex: true, addressless: true },

  // The gallery. A real address rather than a shell, because it is the one
  // wall page worth indexing and a page needs a canonical of its own to be.
  { path: '/open-wall', titleKey: 'title.openwall' },

  // Camera boards from real cameras, read at runtime from /api/v1/boards.
  { path: '/cameras/boards', titleKey: 'pages.boards.title', descriptionKey: 'pages.boards.lede' },

  { path: '/business', titleKey: 'pages.business.title' },
  { path: '/community', titleKey: 'pages.community.title' },
  { path: '/digital-twins', titleKey: 'pages.digital_twins.title' },
  { path: '/donate', titleKey: 'pages.donate.title' },
  { path: '/ecosystem', titleKey: 'pages.ecosystem.title' },
  { path: '/edge-ai', titleKey: 'pages.edge_ai.title', descriptionKey: 'pages.edge_ai.meta_description' },
  // What every build puts on the chip, from the reports the CI pushes (builds/PUSH.md).
  { path: '/firmware-explorer', titleKey: 'pages.firmware_explorer.title', descriptionKey: 'pages.firmware_explorer.lede' },
  { path: '/get-started', titleKey: 'pages.get_started.title' },
  { path: '/green_life', titleKey: 'pages.green_life.title' },
  { path: '/isp-sensors', titleKey: 'pages.isp_sensors.title' },
  { path: '/low-latency', titleKey: 'pages.low_latency.title' },
  { path: '/majestic-endpoints', titleKey: 'pages.majestic_endpoints.title' },
  { path: '/our-team', titleKey: 'pages.our_team.title' },
  { path: '/privacy', titleKey: 'pages.privacy.title' },
  { path: '/reverse-engineering', titleKey: 'pages.reverse_engineering.title' },
  { path: '/stages-of-firmware-development', titleKey: 'pages.stages_of_firmware_development.title' },
  { path: '/teleoperation', titleKey: 'pages.teleoperation.title' },
  { path: '/turnkey-hardware', titleKey: 'pages.turnkey_hardware.title' },
  { path: '/utilities', titleKey: 'pages.utilities.title' },
  { path: '/video-encoding', titleKey: 'pages.video_encoding.title' },
  { path: '/web-interface', titleKey: 'pages.web_interface.title' },

  // The three web tools. Their addresses carry a directory that is not a page:
  // `/tools/` itself has no index.html and must not get one. check-bundle.sh
  // allows a directory that is not empty, and nginx's try_files misses it and
  // falls through to @fallback, which answers it with the 404 page.
  { path: '/tools/firmware-partitions-calculation', titleKey: 'pages.firmware_partitions_calculation.title' },
  { path: '/tools/high-resolution-timer', titleKey: 'pages.high_resolution_timer.title' },
  { path: '/tools/qr-code-generator', titleKey: 'pages.qr_code_generator.title' },
];

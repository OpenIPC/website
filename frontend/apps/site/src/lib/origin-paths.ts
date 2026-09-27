/**
 * Addresses the bundle links to that something other than a bundle page
 * answers: a page the bundle builds from a file route of its own, the Go
 * service, or an nginx location (#160).
 *
 * Every internal link on a static page has to resolve. A link to a path
 * nothing answers falls through to the route map's catch-all, a 302 home,
 * which looks like a working link right up until somebody clicks it.
 *
 * So the check is in two halves that meet here. `pages.build.test.ts` asserts
 * every internal href in the built tree is either a bundle address or one of
 * these; service/deploytest (bundle_test.go) asserts every one of these is
 * answered -- by a Go route, an nginx location or the route map. Neither half
 * can pass while the other is wrong, and neither needs the other's runtime.
 *
 * Prefixes end in `/`. They are for trees nginx serves from disk, where the
 * leaf is a file name rather than an address anything routes.
 */
export const ORIGIN_PATHS: string[] = [
  '/',
  '/open-wall',
  '/supported-hardware',
];

/**
 * The firmware download, one address per SoC (#164), and one snapshot.
 *
 * The download costs about a second of CPU and 8-32MB of disk per call and is
 * guarded by two limit_req zones a file served from the bundle would walk
 * straight past, so it is the Go firmware role's and never a file.
 *
 * Listed as shapes rather than as 126 strings: the deploy test resolves them
 * against service/routes.json, and a shape that stopped matching would fail
 * there. The wizard emits the download link from the browser rather than into
 * the HTML, and the home page's mosaic fills its snapshot links at runtime, so
 * `pages.build.test.ts` meets neither -- which is why the deploy half is the
 * half that matters for these.
 */
export const ORIGIN_PATTERNS: RegExp[] = [
  /^\/cameras\/vendors\/[a-z0-9_-]+\/socs\/[a-z0-9_.-]+\/download_full_image$/,
  /^\/snapshots\/[a-z0-9]+$/,
];

/** Trees served from disk, matched as prefixes. */
export const ORIGIN_PREFIXES: string[] = [
  '/dl/',
  '/wall/',
  // Board photos, dumps and console captures (/cameras/boards), which nginx
  // serves from disk. The island links them at runtime.
  '/board-files/',
];

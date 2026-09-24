/**
 * Addresses the bundle links to and Rails still owns (#160).
 *
 * Every internal link on a static page has to resolve. A link to a path the
 * router does not know falls through the catch-all to a 302 home, which looks
 * like a working link right up until somebody clicks it -- that is what
 * `test/controllers/relaunch_pages_test.rb` has been catching, and the check
 * has to survive the pages leaving Rails.
 *
 * So the check is in two halves that meet here. `pages.build.test.ts` asserts
 * every internal href in the built tree is either a bundle address or one of
 * these; `test/deploy/static_bundle_test.rb` asserts every one of these is a
 * real Rails route. Neither half can pass while the other is wrong, and
 * neither needs the other's runtime.
 *
 * Prefixes end in `/`. They are for trees nginx serves from disk -- the
 * firmware downloads and the wall's images -- where the leaf is a file name
 * rather than an address the router knows, and where the Rails half of the
 * check has nothing to look up.
 */
export const RAILS_PATHS: string[] = [
  '/',
  '/open-wall',
  '/supported-hardware',
];

/**
 * The firmware download, one address per SoC (#164).
 *
 * The wizard itself used to be here: 126 addresses the catalogue linked to and
 * Rails owned. It is in the bundle now, and what is left under that tree is
 * the one action that never leaves Rails -- it costs about a second of CPU and
 * 8-32MB of disk per call and is guarded by two limit_req zones a file served
 * from the bundle would walk straight past.
 *
 * Listed as a shape rather than as 126 strings: the Rails half of the check
 * resolves it against the router, and a shape that stopped matching would fail
 * there. The wizard emits this link from the browser rather than into the
 * HTML, so `pages.build.test.ts` does not meet it -- which is why the Rails
 * half is the half that matters for this one.
 */
export const RAILS_PATTERNS: RegExp[] = [
  /^\/cameras\/vendors\/[a-z0-9_-]+\/socs\/[a-z0-9_.-]+\/download_full_image$/,
];

/** Trees served from disk, matched as prefixes. */
export const RAILS_PREFIXES: string[] = [
  '/dl/',
  '/wall/',
];

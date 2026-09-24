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
 * The wizard, one address per SoC (#162).
 *
 * The catalogue pages are in the bundle and every row of them links to
 * `/cameras/vendors/<vendor>/socs/<soc>`, which is Rails' until #163 -- 126
 * addresses that are real routes and are not files here. Listed as a shape
 * rather than as 126 strings: the Rails half of the check resolves it against
 * the router, and a shape that stopped matching would fail there.
 */
export const RAILS_PATTERNS: RegExp[] = [
  /^\/cameras\/vendors\/[a-z0-9_-]+\/socs\/[a-z0-9_.-]+$/,
];

/** Trees served from disk, matched as prefixes. */
export const RAILS_PREFIXES: string[] = [
  '/dl/',
  '/wall/',
];

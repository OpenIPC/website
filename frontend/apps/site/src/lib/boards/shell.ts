/**
 * The board page's shell: what the web role fills in (service/internal/boards/page.go).
 *
 * A board has an address of its own, /cameras/boards/<id>, so that a search
 * for its printed code can land on it: with ?model= on the catalogue's address
 * every board was the one page /cameras/boards to a search engine. The boards
 * are in PostgreSQL and change between releases, so nothing about them can be
 * prerendered. The bundle builds this page once per language instead, with the
 * board left as the tokens below, and the web role answers each board's
 * address with it: the tokens replaced, and the board written out as HTML
 * where the marker is, for a reader (or a crawler) that runs no script. The
 * island then shows the same board in full.
 *
 * The strings are a contract with page.go, which names them too; the build
 * test holds this side, the Go tests the other.
 */

/**
 * The board's id, wherever the page names its own address: the canonical, the
 * hreflang set, og:url and the language switcher. Shaped like an id, so the
 * shell's own links are ones the link check knows the origin answers.
 */
export const BOARD_ID_TOKEN = 'openipc-shell-board-id';

/** The board's name in the title (`<title>`, og:title); " - OpenIPC" follows it. */
export const BOARD_TITLE_TOKEN = '__OPENIPC_BOARD_TITLE__';

/** The meta description and og:description. */
export const BOARD_DESCRIPTION_TOKEN = '__OPENIPC_BOARD_DESCRIPTION__';

/** Where the board is written out, inside the element with BOARD_SSR_ID. */
export const BOARD_MARKER = '<!--openipc:board-->';

/** The element holding the written-out board, which the island replaces. */
export const BOARD_SSR_ID = 'board-ssr';

/** One board's address, without a locale prefix. */
export const boardPath = (id: string): string => `/cameras/boards/${id}`;

/** The board a path names, or null: /cameras/boards/<id>, with or without a locale prefix. */
export function boardOfPath(pathname: string): string | null {
  const m = /^(?:\/(?:ru|zh))?\/cameras\/boards\/([a-z0-9][a-z0-9-]{0,127})\/?$/.exec(pathname);
  return m ? m[1] : null;
}

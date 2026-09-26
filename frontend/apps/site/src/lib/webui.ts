/**
 * The WebUI screenshots, and where their files are (#160).
 *
 * The manifest is exported from data/webui_gallery.yml -- the same file
 * tools/webui-gallery reads to know which pages of a camera to photograph, so
 * the page and the photographs cannot describe different sets of screens.
 *
 * The images live in src/assets/webui, where tools/webui-gallery installs
 * them. They were globbed out of Rails' app/assets/images until Rails went
 * (#304), so that there was one copy of each while two pages showed them.
 *
 * Two files per screen. The tile is the 1200px copy the page loads; the zoom
 * swaps in the 2560px original, which is what stops the zoom being an upscale
 * of a thumbnail. Both are 2x for the size they are shown at -- a 1x capture
 * is what made the old gallery look soft on every Retina display.
 */
import manifest from '../data/webui-gallery.json';

const FILES = import.meta.glob<ImageMetadata>(
  '../assets/webui/*.webp',
  { eager: true, import: 'default' },
);

export interface Screen {
  slug: string;
  caption: string;
  alt: string;
  tile: ImageMetadata;
  full: ImageMetadata;
}

function file(name: string): ImageMetadata {
  const key = `../assets/webui/${name}`;
  const found = FILES[key];
  if (!found) {
    throw new Error(
      `No WebUI screenshot ${name}. data/webui_gallery.yml names it; `
      + 'run tools/webui-gallery/run.sh, or remove the entry.',
    );
  }
  return found;
}

export const SCREENS: Screen[] = manifest.map((screen) => ({
  ...screen,
  tile: file(`${screen.slug}-thumb.webp`),
  full: file(`${screen.slug}.webp`),
}));

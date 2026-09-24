/**
 * One Bootstrap Icon, inlined, for the wizard island.
 *
 * Icon.astro does the same thing for prerendered pages and cannot be used
 * here: it is an Astro component, and this page's chrome is decided in the
 * browser. Same MIT set, same four glyphs the Rails page draws from the
 * webfont, imported by name so the bundle carries only these.
 *
 * The width and height attributes come off, because the glyph they replace is
 * sized by its font-size -- 1.25rem under `fs-5`, 2.3rem for the GitHub mark,
 * which `i.bi-github` sets over any utility.
 */
import github from '../../assets/icons/github.svg?raw';
import infoCircleFill from '../../assets/icons/info-circle-fill.svg?raw';
import exclamationTriangleFill from '../../assets/icons/exclamation-triangle-fill.svg?raw';
import checkCircleFill from '../../assets/icons/check-circle-fill.svg?raw';

const ICONS: Record<string, string> = {
  github,
  'info-circle-fill': infoCircleFill,
  'exclamation-triangle-fill': exclamationTriangleFill,
  'check-circle-fill': checkCircleFill,
};

/**
 * The box a glyph occupies, which is not the glyph.
 *
 * `<i class="bi bi-info-circle-fill fs-5">` draws a 20px glyph inside a 32px
 * line box -- 1.25rem of font in 1.6 of leading -- and in a `d-flex` alert
 * that line box is what sets the row's height. Sizing the SVG alone left every
 * callout on the page 4px short of the origin's.
 */
const BOXES = {
  // `fs-5`: 1.25rem in 1.6.
  fs5: 'h-8 w-5 [&>svg]:size-5',
  // `fs-4`: 1.5rem in 1.6.
  fs4: 'h-[2.4rem] w-6 [&>svg]:size-6',
  // `i.bi-github`, which _utilities.scss sets to 2.3rem at a line-height of 1.
  github: 'size-[2.3rem] [&>svg]:size-[2.3rem]',
  // The same mark under `fs-5`. Bootstrap's font-size utilities carry
  // `!important`, so the class wins over the element rule and the glyph is
  // 1.25rem -- at a line-height of 1, which the element rule does still set.
  githubFs5: 'size-5 [&>svg]:size-5',
} as const;

export default function Icon({ name, size, class: className = '' }:
{ name: string; size: keyof typeof BOXES; class?: string }) {
  const svg = ICONS[name];
  if (!svg) throw new Error(`No icon "${name}" in the wizard's set.`);

  return (
    <span
      class={`site-icon inline-flex shrink-0 items-center justify-center ${BOXES[size]} ${className}`}
      aria-hidden="true"
      dangerouslySetInnerHTML={{ __html: svg.replace(/\s(?:width|height)="[^"]*"/g, '') }}
    />
  );
}

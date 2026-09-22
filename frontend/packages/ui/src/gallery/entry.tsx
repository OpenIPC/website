/**
 * One page showing every component in the package, server-rendered.
 *
 * Storybook is the tool for working on a component. This is the tool for
 * showing somebody the whole set at once: a single self-contained HTML file
 * with the fonts inlined, which opens from a file:// URL, attaches to a
 * review, and needs nothing running. `npm run gallery` writes it.
 */
import { h } from 'preact';
import type { ComponentChildren, ComponentType } from 'preact';
import * as ui from '../index';
import { SOCS } from '../__fixtures__/socs';
import { TEAM } from '../__fixtures__/team';
import { SUPPORTERS } from '../__fixtures__/supporters';
import { MENU_ITEMS } from '../components/widgets/header-menu/constants';

/**
 * Monogram stand-ins for the two fixtures that point at remote images.
 *
 * The fixtures name real avatars and real project marks, which is right for
 * Storybook. This page is meant to be one file that opens anywhere -- and the
 * artifact viewer that reviews it blocks third-party images outright -- so
 * here they become inline SVG instead of ten requests that may not land.
 */
function monogram(label: string, bg: string, fg = '#ffffff'): string {
  const initials = label.replace(/[^A-Za-z0-9 ]/g, '').split(/\s+/)
    .map(w => w[0]).filter(Boolean).slice(0, 2).join('').toUpperCase();
  const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="160" height="160">'
    + `<rect width="160" height="160" fill="${bg}"/>`
    + `<text x="80" y="102" font-family="sans-serif" font-size="62" font-weight="600"`
    + ` text-anchor="middle" fill="${fg}">${initials}</text></svg>`;
  return 'data:image/svg+xml;utf8,' + encodeURIComponent(svg);
}

const SHADES = ['#4c60d8', '#3949ab', '#052c65', '#138350', '#9e9e9e', '#555555'];

/**
 * Stands in for the host's routing. openipc.org addresses a SoC by the
 * `urlname` its model generates; the package takes the finished href rather
 * than guessing at that, and this is the guess a demo is allowed to make.
 */
const demoHref = (soc: { vendor: string, model: string }) =>
  `/supported-hardware/${soc.vendor.toLowerCase()}/${soc.model.toLowerCase()}`;

const TEAM_OFFLINE = TEAM.map((m, i) => ({
  ...m, imgSrc: monogram(m.name, SHADES[i % SHADES.length]),
}));

const SUPPORTERS_OFFLINE = SUPPORTERS.map((s, i) => ({
  ...s, logoUrl: monogram(s.name, '#f8f9fa', SHADES[i % SHADES.length]),
}));

const camera = {
  soc: 'HI3516EV300 + IMX335',
  date: '2026-09-21 06:15:00 UTC',
  firmware: 'OpenIPC 2.3.05.27-lite, majestic',
  uptime: 19_320,
  socTemp: 44.5,
  resolution: '1920x1080',
  size: 84_624,
};

type Entry = {
  name: string,
  group: string,
  note?: string,
  /** A dark strip behind the component, for the ones drawn in white. */
  onBrand?: boolean,
  /** What the extraction changed about this component, if anything. */
  changed?: string,
  /** Interactive: a static render shows its first paint only. */
  live?: boolean,
  node: ComponentChildren,
};

const C = ui as unknown as Record<string, ComponentType<Record<string, unknown>>>;
const e = (name: string, props: Record<string, unknown> = {}) => h(C[name], props);

export const ENTRIES: Entry[] = [
  // --- primitives ---------------------------------------------------------
  { group: 'Primitives', name: 'MainButton',
    node: h('div', { className: 'flex flex-row flex-wrap items-center gap-2' },
      e('MainButton', { size: 'xs', caption: 'xs', clickHandler: () => {} }),
      e('MainButton', { size: 's', caption: 'Flash', clickHandler: () => {} }),
      e('MainButton', { size: 'm', caption: 'Download full image', clickHandler: () => {} }),
      e('MainButton', { size: 's', caption: 'Disabled', disabled: true, clickHandler: () => {} }),
    ) },
  { group: 'Primitives', name: 'ToggleButton',
    node: h('div', { className: 'flex flex-row flex-wrap items-center gap-2' },
      e('ToggleButton', { size: 'xs', changeHandler: () => {} }),
      e('ToggleButton', { size: 's', checked: true, changeHandler: () => {} }),
      e('ToggleButton', { size: 's', disabled: true, changeHandler: () => {} }),
    ) },
  { group: 'Primitives', name: 'Input', changed: 'iconToooltip was destructured and thrown away; spelt right, and used as the icon title', note: 'default, valid, error, disabled',
    node: h('div', { className: 'flex flex-col gap-3' },
      e('Input', { elemName: 'g1', type: 'text', label: 'MTD device name', state: 'default', placeholder: 'hi_sfc', onInput: () => {} }),
      e('Input', { elemName: 'g2', type: 'text', label: 'Flash size, MB', state: 'valid', value: '16', onInput: () => {} }),
      e('Input', { elemName: 'g3', type: 'text', label: 'Initial offset', state: 'error', value: '0x', errorText: 'Invalid hexademical number', onInput: () => {} }),
      e('Input', { elemName: 'g4', type: 'text', label: 'Not editable', state: 'disabled', value: '—', onInput: () => {} }),
    ) },
  { group: 'Primitives', name: 'Radio',
    node: e('Radio', { name: 'g-radio', captions: ['NOR', 'NAND'], checked: 'NOR', changeHandler: () => {} }) },
  { group: 'Primitives', name: 'CustomSelect', live: true, changed: 'setter and ref renamed; options keyed by value. Both its inputs carried the same name, so a form sent the field twice; neither was ever disabled; the box showed the raw value instead of the option\'s display text; and it opened on click alone with unfocusable options, so a keyboard could not reach it',
    node: e('CustomSelect', {
      state: 'default', value: 'hi_sfc', elemName: 'g-sel', label: 'MTD device name', onChange: () => {},
      options: ['hi_sfc', 'hinand', 'jz_sfc', 'nor-flash'].map(v => ({ value: v, option: v, display: v })),
    }) },
  { group: 'Primitives', name: 'Select',
    node: e('Select', { label: 'Flash size, MB', elemName: 'g-sel2', state: 'default', onInput: () => {},
      options: [{ value: '8' }, { value: '16' }] }) },
  { group: 'Primitives', name: 'Headings',
    node: h('div', {}, e('H1', { content: 'Supported hardware' }), e('H2', { content: 'HiSilicon' })) },

  // --- page furniture -----------------------------------------------------
  { group: 'Page furniture', name: 'HeaderMenu', live: true, changed: 'the labels were white at opacity-60 over the brand blue, 2.95:1 — under the 3:1 large-text floor; undimmed they are 5.29:1, and hover underlines instead. On mobile every link called preventDefault and only closed the drawer, so none of them navigated. preact-iso dropped: the anchor navigates, and the outside-click close no longer looks for the SPA\'s #app', note: 'the site navigation, desktop breakpoint',
    node: e('HeaderMenu', { menuItems: MENU_ITEMS }) },
  { group: 'Page furniture', name: 'Paragraph', changed: 'the optional icon is typed as a component and guarded',
    node: h('div', { className: 'flex flex-col gap-4' },
      e('Paragraph', { content: 'OpenIPC is an alternative operating system for IP cameras. Read the [installation guide](https://openipc.org/get-started).' }),
      e('Paragraph', { size: 'small', content: { h: 'Report issues', p: 'File a bug in the [appropriate repository](https://github.com/OpenIPC/).', dl: true } }),
    ) },
  { group: 'Page furniture', name: 'InformationBanner',
    node: h('div', { className: 'flex flex-col gap-3' },
      e('InformationBanner', { type: 'information', content: { h: 'Before you flash', p: 'Read the whole page. A missing step can brick the camera.' } }),
      e('InformationBanner', { type: 'warning', content: { h: 'No recovery', p: 'This chip has no bootloader recovery. A wrong offset is permanent.' } }),
    ) },
  { group: 'Page furniture', name: 'ChatChannel',
    node: e('ChatChannel', { header: 'OpenIPC Users (EN)', link: 'https://t.me/OpenIPC', text: 'International channel about OpenIPC' }) },
  { group: 'Page furniture', name: 'Socials', changed: 'block and flex were set on the same element', onBrand: false, node: e('Socials') },
  { group: 'Page furniture', name: 'Alliance', node: e('Alliance') },
  { group: 'Page furniture', name: 'Copyright', node: e('Copyright') },
  { group: 'Page furniture', name: 'Disclaimer', node: e('Disclaimer') },
  { group: 'Page furniture', name: 'Footer', onBrand: true, node: e('Footer') },

  // --- hardware catalogue -------------------------------------------------
  { group: 'Hardware catalogue', name: 'AbcSelector', changed: 'the tabs were list items with an onClick: unfocusable, unannounced, unusable without a pointer — which made the catalogue unfilterable from a keyboard. The handler also read the label back out of the DOM instead of being told which tab was clicked', live: true,
    node: e('AbcSelector', { letters: ['A', 'F', 'G', 'H', 'I', 'M', 'N', 'R', 'S', 'T', 'X'], curSelected: 'H', clickHandler: () => {} }) },
  { group: 'Hardware catalogue', name: 'VendorsList', changed: 'same as AbcSelector: keyboard-operable tabs, told which vendor they are', live: true,
    node: e('VendorsList', { list: ['Goke', 'HiSilicon', 'Ingenic', 'SigmaStar', 'Xiongmai'], curSelected: 'HiSilicon', clickHandler: () => {} }) },
  { group: 'Hardware catalogue', name: 'FirmwareDevStages', node: e('FirmwareDevStages') },
  { group: 'Hardware catalogue', name: 'SoCList', changed: 'rows keyed by vendor and model', note: 'ten of the 126 fixture rows',
    node: e('SoCList', { list: SOCS.filter(s => s.vendor === 'HiSilicon').slice(0, 10), hrefFor: demoHref }) },
  { group: 'Hardware catalogue', name: 'SoCManagedList', live: true, changed: 'rows keyed by vendor and model; SoCListItem read window.location during render, and the installation link is now a href the caller supplies — openipc.org addresses SoCs by a urlname slug, which is not the vendor and model as displayed', note: 'the whole catalogue with its A–Z and vendor filters; filtering needs JavaScript, this is its first paint',
    node: e('SoCManagedList', { fullList: SOCS, hrefFor: demoHref }) },

  // --- the wall -----------------------------------------------------------
  { group: 'The Open Wall', name: 'CameraSnapshot', changed: 'was a mock: fixed data, an hls.js stream on localhost:4000, and a loading flag nothing ever set. Now props, and that flag drives the skeleton already written for it',
    node: h('div', { className: 'grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-4' },
      e('CameraSnapshot', camera),
      e('CameraSnapshot', { ...camera, socTemp: undefined }),
      e('CameraSnapshot', { ...camera, loading: true }),
    ) },
  { group: 'The Open Wall', name: 'OpenWallGallery', changed: 'takes camera data rather than a list of ids',
    node: e('OpenWallGallery', {
      cameras: Array.from({ length: 6 }, (_, i) => ({
        id: String(i), ...camera, uptime: 3600 * (6 + i * 7), socTemp: 39 + i * 1.5,
      })),
    }) },

  // --- people and money ---------------------------------------------------
  { group: 'People and money', name: 'Team', changed: 'the social icon name was typed as Github | Telegram, widened to the six marks the icon set carries. A member with an empty social list rendered a stray 0, because `socials.length` is a number', note: 'six of the thirty-six on openipc.org/our-team',
    node: e('Team', { members: TEAM_OFFLINE }) },
  { group: 'People and money', name: 'Supporters', changed: 'read its own constants and eight bundled logos; takes them as props, and the package ships no partner artwork', node: e('Supporters', { supporters: SUPPORTERS_OFFLINE }) },
  { group: 'People and money', name: 'DonateBanner',
    node: h('div', { className: 'flex flex-col gap-4' },
      e('DonateBanner', { size: 'small' }),
      e('DonateBanner', { size: 'big' }),
    ) },
  { group: 'People and money', name: 'Wallets', changed: 'carried three real crypto addresses openipc.org does not publish; takes them as props and ships none', note: 'placeholder addresses: the crypto channels are retired and the package ships none',
    node: e('Wallets', {
      wallets: [
        { title: 'Bitcoin — BTC', address: 'bc1qexampleexampleexampleexampleexampleex', icon: 'Btc' },
        { title: 'TRON (TRC20) — USDT', address: 'TExampleExampleExampleExampleExample', icon: 'Tron' },
      ],
      note: 'Example addresses, for this page only.',
    }) },

  // --- tools --------------------------------------------------------------
  { group: 'Tools', name: 'FirmwarePartitionCalculator', live: true, changed: 'lifted out of sites/main/pages/tools/. A decimal initial offset was read as hex, so 4096 laid out at 0x4096; the exported line dropped the offset entirely and would have been pasted over reserved flash; partition names could contain the comma that separates partitions; a preset merged into the previous layout instead of replacing it; the address columns kept describing a layout after it was edited; nothing was actually debounced; and the map drew reserved flash as free space', note: '/tools/firmware-partitions-calculation — 238 lines of arithmetic whose output is pasted into a bootloader',
    node: e('FirmwarePartitionCalculator') },
  { group: 'Tools', name: 'QrCodeWidget', changed: 'encoded in an effect and drew by mutating the SVG through a ref, so a server render emitted an empty square — this one is drawn here, with no JavaScript at all. Text past a QR code\'s capacity threw RangeError out of the effect; it now says so',
    node: h('div', { className: 'max-w-48' }, e('QrCodeWidget', { textToCode: 'https://openipc.org' })) },
  { group: 'Tools', name: 'HighResTimer', live: true, node: e('HighResTimer') },
  { group: 'Tools', name: 'WannabeKey', node: e('WannabeKey') },
  { group: 'Tools', name: 'ModalImage', live: true, changed: 'restored invented body styles rather than the ones it replaced; Escape kept calling whichever close was passed on mount; and two open at once fought over the scroll lock, which is now shared and counted',
    note: 'the lightbox behind the WebUI gallery. It is position: fixed, so the frame below gives it a containing block rather than letting it cover this page',
    node: h('div', {
      className: 'relative h-72 overflow-hidden rounded border border-wallet-border',
      // A transform makes this element the containing block for the modal's
      // fixed positioning, which is the whole reason the frame is here.
      style: 'transform: translateZ(0)',
    },
      e('ModalImage', {
        src: 'data:image/svg+xml;utf8,' + encodeURIComponent(
          '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360">'
          + '<rect width="640" height="360" fill="#e9ecef"/>'
          + '<text x="320" y="190" font-family="sans-serif" font-size="22"'
          + ' text-anchor="middle" fill="#555555">WebUI screenshot</text></svg>'),
        alt: 'A WebUI screenshot', close: () => {},
      })) },
];

export const GROUPS = [...new Set(ENTRIES.map(x => x.group))];

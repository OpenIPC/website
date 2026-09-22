/**
 * Smoke: every exported component renders, on the server and in a browser.
 *
 * Deliberately blunt. It is not checking what the components look like -- that
 * is Storybook's job -- it is checking that none of them reaches for `window`
 * or `document` while rendering, which is the failure that makes a component
 * unusable in a prerendered page and which three of these did on arrival.
 */
import { expect, test, describe } from 'vitest';
import { render } from '@testing-library/preact';
import { renderToString } from 'preact-render-to-string';
import type { ComponentType } from 'preact';
import { h } from 'preact';

import * as ui from '../index';
import { SOCS } from '../__fixtures__/socs';
import { TEAM } from '../__fixtures__/team';
import { SUPPORTERS } from '../__fixtures__/supporters';
import { MENU_ITEMS } from '../components/widgets/header-menu/constants';

const camera = {
  soc: 'HI3516EV300 + IMX335',
  date: '2026-09-21 06:15:00 UTC',
  firmware: 'OpenIPC 2.3.05.27-lite, majestic',
  uptime: 19_320,
  socTemp: 44.5,
  resolution: '1920x1080',
  size: 84624,
};

const props: Record<string, Record<string, unknown>> = {
  IconButton: { Icon: () => h('svg', {}), clickHandler: () => {} },
  MainButton: { size: 's', caption: 'Flash', clickHandler: () => {} },
  ToggleButton: { size: 's', changeHandler: () => {} },
  CustomSelect: {
    state: 'default', value: 'a', elemName: 'sel', onChange: () => {},
    options: [{ value: 'a', option: 'a', display: 'a' }],
  },
  Input: { elemName: 'i', type: 'text', label: 'Label', state: 'default', onInput: () => {} },
  Radio: { name: 'r', captions: ['one', 'two'], checked: 'one', changeHandler: () => {} },
  Select: {
    label: 'L', elemName: 's', state: 'default', onInput: () => {},
    options: [{ value: 'a' }],
  },
  H1: { content: 'Heading' },
  H2: { content: 'Heading' },
  Header: {},
  HeaderMenu: { menuItems: MENU_ITEMS },
  HeaderBurgerButton: { isOpen: false, clickHandler: () => {} },
  Paragraph: { content: 'Some text with a [link](https://openipc.org).' },
  ModalImage: { src: 'x.webp', alt: 'x', close: () => {} },
  DonateBanner: { size: 'small' },
  InformationBanner: { content: 'Heads up.', type: 'information' },
  ChatChannel: { header: 'OpenIPC Users (EN)', link: 'https://t.me/OpenIPC', text: 'Channel' },
  AbcSelector: { letters: ['A', 'H'], curSelected: 'A', clickHandler: () => {} },
  VendorsList: { list: ['HiSilicon'], curSelected: null, clickHandler: () => {} },
  SoCList: { list: SOCS.slice(0, 5) },
  SoCListItem: { ...SOCS[0] },
  SoCManagedList: { fullList: SOCS },
  CameraSnapshot: camera,
  OpenWallGallery: { cameras: [{ id: '1', ...camera }] },
  Team: { members: TEAM },
  TeamMember: { ...TEAM[0] },
  Supporters: { supporters: SUPPORTERS },
  Wallet: { title: 'Bitcoin', address: 'bc1qexample', icon: 'Btc' },
  Wallets: { wallets: [{ title: 'Bitcoin', address: 'bc1qexample', icon: 'Btc' }] },
  QrCodeWidget: { textToCode: 'https://openipc.org' },
};

const components = Object.entries(ui).filter(
  ([name, value]) => typeof value === 'function' && /^[A-Z]/.test(name),
) as [string, ComponentType<Record<string, unknown>>][];

test('the entry point exports the components this suite thinks it does', () => {
  expect(components.length).toBeGreaterThanOrEqual(30);
});

test('no capitalised export is skipped for not being a function', () => {
  // The filter above is how a barrel that exported a namespace object instead
  // of its component slipped through unrendered.
  const capitalised = Object.keys(ui).filter(name => /^[A-Z]/.test(name));
  expect(components.map(([name]) => name).sort()).toEqual(capitalised.sort());
});

describe.each(components)('%s', (name, Component) => {
  const args = props[name] ?? {};

  test('renders to a string with no DOM present', () => {
    expect(renderToString(h(Component, args))).toBeTypeOf('string');
  });

  test('renders into a document', () => {
    const { container } = render(h(Component, args));
    expect(container).toBeDefined();
  });
});

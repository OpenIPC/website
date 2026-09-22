/**
 * The findings from Qodo's review of PR #260 that are not covered where the
 * component itself is tested. Each name is the failure, not the fix.
 */
import { expect, test, describe } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/preact';
import { h } from 'preact';
import { renderToString } from 'preact-render-to-string';
import { readdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import DesktopMenuItem from '../components/widgets/header-menu/components/desktop-menu/components/menu-item';
import { MENU_ITEMS } from '../components/widgets/header-menu/constants';
import {
  Paragraph, IconButton, Radio, Input, Select, CustomSelect, SoCListItem, SoCList,
  QrCodeWidget, HeaderBurgerButton, AbcSelector, VendorsList, TeamMember, ModalImage,
  MainButton, FirmwarePartitionCalculator, ToggleButton,
} from '../index';
import { SOCS } from '../__fixtures__/socs';

const src = join(dirname(fileURLToPath(import.meta.url)), '..');

describe('Paragraph does not turn content into a script', () => {
  const linkIn = (content: string) => {
    const { container } = render(h(Paragraph, { content }));
    return container.querySelector('a');
  };

  test.each([
    'javascript:alert(1)',
    'JaVaScRiPt:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    'vbscript:msgbox(1)',
  ])('drops %s and keeps the text', (url) => {
    const { container } = render(h(Paragraph, { content: `see [this](${url})` }));
    expect(container.querySelector('a')).toBeNull();
    expect(container.textContent).toContain('this');
  });

  test.each([
    'https://openipc.org',
    'http://openipc.org',
    'mailto:hello@openipc.org',
    '/get-started',
    '#section',
    './relative',
  ])('keeps %s', (url) => {
    expect(linkIn(`see [this](${url})`)?.getAttribute('href')).toBe(url);
  });

  test('a protocol-relative URL is not a document link', () => {
    expect(linkIn('see [this](//evil.example/x)')).toBeNull();
  });
});

describe('form controls behave as form controls', () => {
  test('IconButton does not submit the form around it', () => {
    const { container } = render(h(IconButton, { clickHandler: () => {}, children: 'x' }));
    expect(container.querySelector('button')?.getAttribute('type')).toBe('button');
  });

  test('Input passes required through to the control', () => {
    const { container } = render(h(Input, {
      elemName: 'a', type: 'text', label: 'A', state: 'default' as const,
      required: true, onInput: () => {},
    }));
    expect((container.querySelector('input') as HTMLInputElement).required).toBe(true);
  });

  test('an Input controlled to the empty string clears', () => {
    const props = { elemName: 'a', type: 'text', label: 'A', state: 'default' as const, onInput: () => {} };
    const { container, rerender } = render(h(Input, { ...props, value: 'something' }));
    expect((container.querySelector('input') as HTMLInputElement).value).toBe('something');

    rerender(h(Input, { ...props, value: '' }));
    expect((container.querySelector('input') as HTMLInputElement).value).toBe('');
  });

  test('Select carries id, name and required', () => {
    const { container } = render(h(Select, {
      label: 'Flash size', elemName: 'flash-size', state: 'default' as const, required: true,
      onInput: () => {}, options: [{ value: '8' }, { value: '16' }],
    }));
    const select = container.querySelector('select') as HTMLSelectElement;
    expect(select.id).toBe('flash-size');
    expect(select.name).toBe('flash-size');
    expect(select.required).toBe(true);
  });

  test('two Radio groups sharing a caption do not share ids', () => {
    const { container } = render(h('div', {},
      h(Radio, { name: 'first', captions: ['on', 'off'], defaultChecked: 0, changeHandler: () => {} }),
      h(Radio, { name: 'second', captions: ['on', 'off'], defaultChecked: 0, changeHandler: () => {} }),
    ));
    const ids = [...container.querySelectorAll('input')].map(i => i.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  test('picking a CustomSelect option tells the parent', () => {
    let seen: string | undefined;
    const { container } = render(h(CustomSelect, {
      state: 'default' as const, value: 'a', elemName: 'sel',
      onChange: (e: Event) => { seen = (e.target as HTMLInputElement).value; },
      options: [
        { value: 'a', option: 'a', display: 'a' },
        { value: 'b', option: 'b', display: 'b' },
      ],
      open: true,
    }));
    // It dispatched an `input` event at a listener registered with onChange,
    // which in Preact is the native `change` event, so nothing was heard.
    const option = [...container.querySelectorAll('li')].find(li => li.textContent === 'b');
    fireEvent.click(option as HTMLElement);
    expect(seen).toBe('b');
  });
});

describe('the catalogue does not invent routes', () => {
  const soc = SOCS.find(s => s.firmware && s.address)!;

  test('SoCListItem links only where the caller says', () => {
    const { container } = render(h(SoCListItem, { ...soc, href: '/supported-hardware/x/y' }));
    expect(container.querySelector('a')?.getAttribute('href')).toBe('/supported-hardware/x/y');
  });

  test('with no href it states support without linking', () => {
    const { container } = render(h(SoCListItem, { ...soc }));
    expect(container.querySelector('a')).toBeNull();
    expect(container.textContent).toBeTruthy();
  });

  test('SoCList passes hrefFor down to every row', () => {
    const list = SOCS.filter(s => s.firmware && s.address).slice(0, 3);
    const { container } = render(h(SoCList, {
      list, hrefFor: (s: typeof list[number]) => `/hw/${s.model.toLowerCase()}`,
    }));
    const hrefs = [...container.querySelectorAll('a')].map(a => a.getAttribute('href'));
    expect(hrefs).toEqual(list.map(s => `/hw/${s.model.toLowerCase()}`));
  });
});

describe('second round of the review', () => {
  test('the QR code is drawn in the render, not an effect', () => {
    // Effects do not run on the server, so drawing there left prerendered
    // pages and no-JavaScript visitors with an empty square.
    const html = renderToString(h(QrCodeWidget, { textToCode: 'https://openipc.org' }));
    const d = /<path d="([^"]+)"/.exec(html)?.[1] ?? '';
    expect(d.length).toBeGreaterThan(100);
  });

  test('text too long for a QR code says so instead of throwing', () => {
    const tooLong = 'x'.repeat(10_000);
    expect(() => renderToString(h(QrCodeWidget, { textToCode: tooLong }))).not.toThrow();
    expect(renderToString(h(QrCodeWidget, { textToCode: tooLong }))).toContain('Too much text');
  });

  test('the burger button is a button and says whether it is open', () => {
    const { container } = render(h(HeaderBurgerButton, { isOpen: false, clickHandler: () => {} }));
    const button = container.querySelector('button');
    expect(button).not.toBeNull();
    expect(button?.getAttribute('type')).toBe('button');
    expect(button?.getAttribute('aria-expanded')).toBe('false');
  });

  test.each([
    ['AbcSelector', () => h(AbcSelector, { letters: ['A', 'H'], curSelected: 'A', clickHandler: () => {} })],
    ['VendorsList', () => h(VendorsList, { list: ['HiSilicon'], curSelected: null, clickHandler: () => {} })],
  ])('%s filters are reachable from the keyboard', (_name, build) => {
    const { container } = render(build());
    const tabs = [...container.querySelectorAll('[role="tab"]')];
    expect(tabs.length).toBeGreaterThan(0);
    expect(tabs.every(t => t.getAttribute('tabindex') === '0')).toBe(true);
  });

  test('Enter on a filter selects it', () => {
    let chosen: string | undefined;
    const { container } = render(h(AbcSelector, {
      letters: ['A', 'H'], curSelected: null, clickHandler: (l: string) => { chosen = l; },
    }));
    const tab = [...container.querySelectorAll('[role="tab"]')].find(t => t.textContent === 'H');
    fireEvent.keyDown(tab as HTMLElement, { key: 'Enter' });
    expect(chosen).toBe('H');
  });

  test('CustomSelect submits one value, not two', () => {
    const { container } = render(h(CustomSelect, {
      state: 'default' as const, value: 'a', elemName: 'sel', onChange: () => {},
      options: [{ value: 'a', option: 'A', display: 'A' }],
    }));
    const named = [...container.querySelectorAll('[name="sel"]')];
    expect(named.length).toBe(1);
  });

  test('CustomSelect shows the option display text, not the raw value', () => {
    const { container } = render(h(CustomSelect, {
      state: 'default' as const, value: '8', elemName: 'flash', onChange: () => {},
      options: [{ value: '8', option: 'NOR 8 MB', display: 'NOR 8' }],
    }));
    const box = container.querySelector('#flash') as HTMLInputElement;
    expect(box.value).toBe('NOR 8');
  });

  test('a disabled CustomSelect contributes nothing to a submission', () => {
    const { container } = render(h(CustomSelect, {
      state: 'disabled' as const, value: 'a', elemName: 'sel', onChange: () => {},
      options: [{ value: 'a', option: 'A', display: 'A' }],
    }));
    const field = container.querySelector('[name="sel"]') as HTMLInputElement;
    expect(field.disabled).toBe(true);
  });

  test('a team member with no socials renders no stray zero', () => {
    // `socials && socials.length && ...` evaluates to the number 0.
    const { container } = render(h(TeamMember, {
      name: 'Nobody', bio: 'Contributor', socials: [],
    }));
    expect(container.textContent).not.toContain('0');
  });

  test('Escape closes with the current callback, not the one from mount', () => {
    let first = 0, second = 0;
    const { rerender } = render(h(ModalImage, {
      src: 'x.webp', alt: 'x', close: () => { first++; },
    }));
    rerender(h(ModalImage, { src: 'x.webp', alt: 'x', close: () => { second++; } }));

    fireEvent.keyUp(document, { code: 'Escape' });
    expect(second).toBe(1);
    expect(first).toBe(0);
  });
});

describe('third round of the review', () => {
  test('the published declarations reference no file the build drops', () => {
    // Nineteen hand-written .d.ts files were imported by the emitted
    // declarations and never emitted themselves, so every one of those
    // imports dangled. They are ordinary .ts now, and
    // scripts/verify-consumable.sh compiles a consumer with skipLibCheck
    // off, which is what would have caught it.
    const p = join(src, 'components');
    const handWritten: string[] = [];
    const walk = (dir: string) => {
      for (const e of readdirSync(dir, { withFileTypes: true })) {
        if (e.isDirectory()) walk(join(dir, e.name));
        else if (e.name.endsWith('.d.ts')) handWritten.push(join(dir, e.name));
      }
    };
    walk(p);
    walk(join(src, 'utils'));
    expect(handWritten).toEqual([]);
  });

  test('a parent menu entry can be opened from the keyboard', () => {
    const parent = MENU_ITEMS.find(i => i.children?.length)!;
    const { container } = render(h(DesktopMenuItem, { menuItem: parent, active: false }));
    const button = container.querySelector('button');
    expect(button).not.toBeNull();
    expect(button?.getAttribute('aria-expanded')).toBe('false');

    fireEvent.click(button as HTMLElement);
    expect(container.querySelector('button')?.getAttribute('aria-expanded')).toBe('true');
  });

  test('the modal close control is a labelled button', () => {
    const { container } = render(h(ModalImage, { src: 'x.webp', alt: 'x', close: () => {} }));
    const button = [...container.querySelectorAll('button')]
      .find(b => b.getAttribute('aria-label') === 'Close image');
    expect(button).toBeDefined();
    expect(button?.getAttribute('type')).toBe('button');
  });

  test('an icon-only button says what it is', () => {
    const { container } = render(h(MainButton, {
      size: 's', Icon: () => h('svg', {}), label: 'Pause the timer', clickHandler: () => {},
    }));
    expect(container.querySelector('button')?.getAttribute('aria-label')).toBe('Pause the timer');
  });

  test("an Input's icon action is a button when it does something", () => {
    const withAction = render(h(Input, {
      elemName: 'mac', type: 'text', label: 'MAC', state: 'default' as const, onInput: () => {},
      Icon: () => h('svg', {}), iconClickHandler: () => {}, iconLabel: 'Generate a random MAC',
    }));
    const button = withAction.container.querySelector('button');
    expect(button?.getAttribute('aria-label')).toBe('Generate a random MAC');

    // A decorative icon stays a div: there is nothing to activate.
    const decorative = render(h(Input, {
      elemName: 'other', type: 'text', label: 'Other', state: 'default' as const,
      onInput: () => {}, Icon: () => h('svg', {}),
    }));
    expect(decorative.container.querySelector('button')).toBeNull();
  });

  test('reserved flash is drawn on the partition map, not left looking free', () => {
    render(h(FirmwarePartitionCalculator, {}));
    const lite = screen.getAllByText('Lite')[0];
    fireEvent.click(lite);
    const part3 = document.querySelector('input[name="part3-size"]') as HTMLInputElement;
    fireEvent.input(part3, { target: { value: '4864' } });     // free 256 KB
    const offset = document.querySelector('input[name="initial-offset"]') as HTMLInputElement;
    fireEvent.input(offset, { target: { value: '0x40000' } }); // reserve it

    // Free space is zero, so nothing in the bar may read as available.
    expect(document.querySelector('span')?.textContent).toBe('Free space: 0 KB');
    expect(document.body.innerHTML).toContain('bg-dark-grey');
  });
});

describe('fourth round of the review', () => {
  test('a CustomSelect shows its display text on the server', () => {
    // The effect that corrects this does not run in a prerender, so the
    // static page showed 8 where 'NOR 8' was configured.
    const html = renderToString(h(CustomSelect, {
      state: 'default' as const, value: '8', elemName: 'flash', onChange: () => {},
      options: [{ value: '8', option: 'NOR 8 MB', display: 'NOR 8' }],
    }));
    expect(html).toContain('NOR 8');
  });

  test('a CustomSelect label names the control it labels', () => {
    const { container } = render(h(CustomSelect, {
      state: 'default' as const, value: 'a', elemName: 'sel', label: 'Flash size',
      onChange: () => {}, options: [{ value: 'a', option: 'A', display: 'A' }],
    }));
    expect(container.querySelector('label')?.getAttribute('for')).toBe('sel');
  });

  test('a CustomSelect disabled while open cannot still be changed', () => {
    let changed = 0;
    const { container } = render(h(CustomSelect, {
      state: 'disabled' as const, value: 'a', elemName: 'sel', open: true,
      onChange: () => { changed++; },
      options: [
        { value: 'a', option: 'A', display: 'A' },
        { value: 'b', option: 'B', display: 'B' },
      ],
    }));
    const option = [...container.querySelectorAll('li')].find(li => li.textContent === 'B');
    if (option) fireEvent.click(option);
    expect(changed).toBe(0);
  });

  test('an icon-only toggle says what it toggles', () => {
    const { container } = render(h(ToggleButton, {
      size: 's', label: 'Pause', Icon: () => h('svg', {}), changeHandler: () => {},
    }));
    expect(container.querySelector('input')?.getAttribute('aria-label')).toBe('Pause');
  });

  test('a profile portrait has a text alternative', () => {
    const { container } = render(h(TeamMember, {
      name: 'widgetii', bio: 'Majestic Streamer', imgSrc: 'x.png', socials: [],
    }));
    expect(container.querySelector('img')?.getAttribute('alt')).toBe('widgetii');
  });

  test('the partition map of a full chip sums to 100%, not 101%', () => {
    // 256 + 64 + 2048 + 5120 + 704 KB rounded one at a time came to 101% of
    // the 8 MB they exactly fill, and the bar clips at overflow-hidden -- so
    // a layout that fitted perfectly lost the end of its last partition.
    render(h(FirmwarePartitionCalculator, {}));
    fireEvent.click(screen.getAllByText('Lite')[0]);

    const widths = [...document.querySelectorAll('[style*="width:"]')]
      .map(el => /width:\s*([\d.]+)%/.exec(el.getAttribute('style') ?? '')?.[1])
      .filter(Boolean)
      .map(Number);

    expect(widths.length).toBe(5);
    expect(widths.reduce((a, b) => a + b, 0)).toBe(100);
  });
});

/**
 * The findings from Qodo's review of PR #260 that are not covered where the
 * component itself is tested. Each name is the failure, not the fix.
 */
import { expect, test, describe } from 'vitest';
import { render, fireEvent } from '@testing-library/preact';
import { h } from 'preact';
import {
  Paragraph, IconButton, Radio, Input, Select, CustomSelect, SoCListItem, SoCList,
} from '../index';
import { SOCS } from '../__fixtures__/socs';

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

import { describe, expect, test } from 'vitest';
import { eventFor, initEvents } from './events';

const link = (attrs: Record<string, string>) => ({
  getAttribute: (name: string) => attrs[name] ?? null,
});

describe('eventFor', () => {
  test('a named link counts its own name, wherever it points', () => {
    expect(eventFor(link({ 'data-event': 'business-mail', href: 'mailto:business@openipc.org' }), 'openipc.org'))
      .toBe('business-mail');
    expect(eventFor(link({ 'data-event': 'oc-checkout', href: 'https://opencollective.com/openipc' }), 'openipc.org'))
      .toBe('oc-checkout');
  });

  test('an unnamed link to another host counts by host', () => {
    expect(eventFor(link({ href: 'https://github.com/OpenIPC/firmware' }), 'openipc.org')).toBe('ext:github.com');
    expect(eventFor(link({ href: 'HTTP://t.me/openipc' }), 'openipc.org')).toBe('ext:t.me');
  });

  test('links that stay on the site, and non-web links, count nothing', () => {
    expect(eventFor(link({ href: 'https://openipc.org/donate' }), 'openipc.org')).toBeNull();
    expect(eventFor(link({ href: '/donate' }), 'openipc.org')).toBeNull();
    expect(eventFor(link({ href: '#top' }), 'openipc.org')).toBeNull();
    expect(eventFor(link({ href: 'mailto:x@example.com' }), 'openipc.org')).toBeNull();
    expect(eventFor(link({ href: 'https://' }), 'openipc.org')).toBeNull();
  });

  test('on a mirror, the mirror is "the site" and the origin is not', () => {
    expect(eventFor(link({ href: 'https://openipc.ru/ru/donate' }), 'openipc.ru')).toBeNull();
    expect(eventFor(link({ href: 'https://openipc.org/' }), 'openipc.ru')).toBe('ext:openipc.org');
  });
});

describe('initEvents', () => {
  // A click lands on whatever is under the pointer -- often an icon or a span
  // inside the link -- and is found from there with closest().
  const clickOn = (found: unknown) => {
    let listener: ((ev: Event) => void) | undefined;
    const counted: string[] = [];
    initEvents({ addEventListener: ((_: string, l: (ev: Event) => void) => { listener = l; }) as never }, 'openipc.org',
      (name) => counted.push(name));
    listener!({ target: { closest: (sel: string) => (sel === 'a[href]' ? found : null) } } as unknown as Event);
    return counted;
  };

  test('one click on a named link counts exactly one event', () => {
    expect(clickOn(link({ 'data-event': 'download-step:business:fpv', href: '/business' })))
      .toEqual(['download-step:business:fpv']);
  });

  test('a click outside any link counts nothing', () => {
    expect(clickOn(null)).toEqual([]);
  });
});

// @vitest-environment jsdom
/** The one-question strip (#185): asks once, counts an answer, counts nothing on close. */
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { act, cleanup, render } from '@testing-library/preact';
import { KEY, dayOf } from '../lib/why';
import WhyStrip from './WhyStrip.tsx';

const labels = {
  question: 'What brought you here today?', note: 'One tap.', how: 'What we count', close: 'Close',
  thanks: 'Thank you.',
  answers: { flash: 'Flash', fpv: 'FPV', hardware: 'Hardware', product: 'Product', looking: 'Looking' },
};

type Count = (name: string) => void;
let count: ReturnType<typeof vi.fn<Count>>;

beforeEach(() => {
  // Before ENDS, whatever day the suite runs on.
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(2026, 10, 2, 12));
  count = vi.fn<Count>();
  window.openipcCount = count;
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
  localStorage.clear();
  delete window.openipcCount;
});

function strip(locale = 'en'): Element {
  let box: Element | undefined;
  act(() => { box = render(<WhyStrip locale={locale} labels={labels} privacyHref="/privacy" />).container; });
  return box!;
}

test('asks a new browser, and notes that it did', () => {
  const box = strip();
  expect(box.textContent).toContain('What brought you here today?');
  expect(box.querySelector('a')!.getAttribute('href')).toBe('/privacy');
  expect(localStorage.getItem(KEY)).toBe(`shown:${dayOf(new Date())}`);
});

test('an answer counts why:<answer>:<locale> once and thanks', () => {
  const box = strip('zh');
  const fpv = [...box.querySelectorAll('button')].find((b) => b.textContent === 'FPV')!;
  act(() => { fpv.click(); });
  expect(count).toHaveBeenCalledTimes(1);
  expect(count).toHaveBeenCalledWith('why:fpv:zh');
  expect(box.textContent).toContain('Thank you.');
  expect(box.querySelectorAll('button')).toHaveLength(0);
  expect(localStorage.getItem(KEY)).toBe('answered');
});

test('closing counts nothing and is remembered', () => {
  const box = strip();
  act(() => { box.querySelector<HTMLButtonElement>('button[aria-label="Close"]')!.click(); });
  expect(count).not.toHaveBeenCalled();
  expect(box.textContent).toBe('');
  expect(localStorage.getItem(KEY)).toBe(`dismissed:${dayOf(new Date())}`);
});

test('a browser asked today, or one that answered, sees nothing', () => {
  localStorage.setItem(KEY, `shown:${dayOf(new Date())}`);
  expect(strip().textContent).toBe('');
  cleanup();
  localStorage.setItem(KEY, 'answered');
  expect(strip().textContent).toBe('');
});

test('where storage is refused it does not ask', () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('denied'); });
  expect(strip().textContent).toBe('');
});

test('where the note cannot be written it does not ask', () => {
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('quota'); });
  expect(strip().textContent).toBe('');
});

test('without the sender an answer still thanks', () => {
  delete window.openipcCount;
  const box = strip();
  act(() => { box.querySelector<HTMLButtonElement>('.rounded-full')!.click(); });
  expect(box.textContent).toContain('Thank you.');
});

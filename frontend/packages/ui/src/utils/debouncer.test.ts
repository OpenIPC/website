import { expect, test, vi, beforeEach, afterEach } from 'vitest';
import { debounce } from './debouncer';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

test('fires once, after the wait, with the last arguments', () => {
  const spy = vi.fn();
  const [debounced] = debounce(spy, 500);

  debounced('a');
  debounced('b');
  debounced('c');
  expect(spy).not.toHaveBeenCalled();

  vi.advanceTimersByTime(499);
  expect(spy).not.toHaveBeenCalled();

  vi.advanceTimersByTime(1);
  expect(spy).toHaveBeenCalledExactlyOnceWith('c');
});

test('hands back the timer handle so a caller can cancel', () => {
  const spy = vi.fn();
  const [debounced, timeout] = debounce(spy, 500);

  debounced();
  clearTimeout(timeout.current);
  vi.advanceTimersByTime(5000);
  expect(spy).not.toHaveBeenCalled();
});

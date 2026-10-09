import { expect, test } from 'vitest';
import { logFirst } from './crashes';

test("majestic's log section comes first, the rest in their order", () => {
  const text = '==> majestic.dump <==\nsignal=11\n\n==> log <==\n05:52:16 INFO <majestic> start\n\n==> threads <==\n1 majestic\n';
  expect(logFirst(text)).toBe('==> log <==\n05:52:16 INFO <majestic> start\n\n==> majestic.dump <==\nsignal=11\n\n==> threads <==\n1 majestic\n');
});

test('a text with no log section, or one already first, is left as it is', () => {
  const kernel = '==> dmesg-ramoops-0 <==\nOops\n==> dmesg-ramoops-1 <==\nPanic\n';
  expect(logFirst(kernel)).toBe(kernel);
  const first = '==> log <==\nline\n==> threads <==\n1 majestic\n';
  expect(logFirst(first)).toBe(first);
});

test('a log section at the very end gains the line break it lacked', () => {
  expect(logFirst('==> majestic.dump <==\nsignal=6\n==> log <==\nlast line')).toBe('==> log <==\nlast line\n==> majestic.dump <==\nsignal=6\n');
});

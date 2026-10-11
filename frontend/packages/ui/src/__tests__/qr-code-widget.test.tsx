/** The Wi-Fi QR code's widget: what #441 found wrong with it. */
import { expect, test } from 'vitest';
import { h } from 'preact';
import { renderToString } from 'preact-render-to-string';
import { QrCodeWidget } from '../index';

test('the empty state is a placeholder, not a warning', () => {
  const html = renderToString(h(QrCodeWidget, { textToCode: '', emptyText: 'Type a network name' }));
  expect(html).toContain('Type a network name');
  expect(html).not.toContain('warning');
});

test('the code is labelled without the text it encodes', () => {
  const html = renderToString(h(QrCodeWidget, {
    textToCode: 'WIFI:S:Home;T:WPA;P:hunter2hunter2;;',
    label: 'QR code for network "Home"',
  }));
  expect(html).toContain('aria-label="QR code for network &quot;Home&quot;"');
  expect(html).not.toContain('hunter2');
});

test('an unlabelled code says only that it is one', () => {
  const html = renderToString(h(QrCodeWidget, { textToCode: 'secret' }));
  expect(html).toContain('aria-label="QR code"');
  expect(html).not.toContain('secret');
});

test('the messages are the page\'s, in its language', () => {
  const tooLong = 'x'.repeat(10_000);
  const html = renderToString(h(QrCodeWidget, { textToCode: tooLong, tooLongText: 'Слишком длинно' }));
  expect(html).toContain('Слишком длинно');
  expect(html).toContain('warning');
});

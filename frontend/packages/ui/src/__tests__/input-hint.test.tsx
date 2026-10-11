/** Input's error and description reach a screen reader with the field (#441, PR #444). */
import { expect, test } from 'vitest';
import { h } from 'preact';
import { renderToString } from 'preact-render-to-string';
import { Input } from '../index';

const base = { elemName: 'pw', label: 'Password', type: 'password', onInput: () => {} };

test('an error marks the field invalid and is its description', () => {
  const html = renderToString(h(Input, { ...base, state: 'error', errorText: 'Too short' }));
  expect(html).toContain('aria-invalid="true"');
  expect(html).toContain('aria-describedby="pw-hint"');
  expect(html).toMatch(/<span id="pw-hint"[^>]*>Too short<\/span>/);
});

test('a field with nothing under it claims neither', () => {
  const html = renderToString(h(Input, { ...base, state: 'default' }));
  expect(html).not.toContain('aria-invalid');
  expect(html).not.toContain('aria-describedby');
});

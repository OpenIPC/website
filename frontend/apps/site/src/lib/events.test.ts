/**
 * The two inline scripts that count clicks and landing tags (#360), run as
 * shipped: layouts/Base.astro inlines these files, and this reads the same
 * files rather than a copy, so the test cannot drift from what a reader runs.
 */
import { describe, expect, test } from 'vitest';
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const BEACON = readFileSync(join(here, 'beacon.inline.js'), 'utf8');
const LANDING = readFileSync(join(here, 'landing.inline.js'), 'utf8');

type Listener = (ev?: unknown) => void;

/** A page with just enough browser in it for the two scripts. */
function page(href = 'https://openipc.org/donate', { countReady = false } = {}) {
  const docListeners: Record<string, Listener[]> = {};
  const winListeners: Record<string, Listener[]> = {};
  const beacons: string[] = [];
  const counted: string[] = [];
  let address = href;

  const location = { get href() { return address; }, get hostname() { return new URL(address).hostname; } };
  const window: Record<string, unknown> = {};
  const document = { addEventListener: (t: string, l: Listener) => { (docListeners[t] ??= []).push(l); } };
  const navigator = { sendBeacon: (url: string) => { beacons.push(url); return true; } };
  const history = { state: { k: 1 }, replaceState: (_s: unknown, _t: string, to: string) => { address = new URL(to, address).href; } };
  const addEventListener = (t: string, l: Listener) => { (winListeners[t] ??= []).push(l); };

  const run = (src: string) => new Function(
    'window', 'document', 'navigator', 'location', 'history', 'addEventListener', 'URL', src,
  )(window, document, navigator, location, history, addEventListener, URL);

  run(BEACON);
  const gc = window.goatcounter as { count?: (v: { path: string }) => void };
  // count.js, arriving: it attaches count() to the object the page set up.
  const countJsRuns = () => { gc.count = (v) => counted.push(v.path); };
  if (countReady) countJsRuns();

  const link = (attrs: Record<string, string>) => ({ getAttribute: (n: string) => attrs[n] ?? null });
  const click = (attrs: Record<string, string> | null) => {
    const found = attrs && link(attrs);
    for (const l of docListeners.click ?? []) l({ target: { closest: (sel: string) => (sel === 'a[href]' ? found : null) } });
  };
  const fire = (t: string) => { for (const l of [...(docListeners[t] ?? []), ...(winListeners[t] ?? [])]) l(); };

  return {
    window, gc, counted, beacons, click, fire, countJsRuns,
    landing: () => run(LANDING),
    get address() { return address; },
  };
}

const beaconName = (url: string) => new URL(url, 'https://openipc.org').searchParams.get('p');

describe('the click sender', () => {
  test('sets up the object count.js attaches to, pointed at the endpoint', () => {
    expect(page().gc).toEqual({ endpoint: '/api/a/count' });
  });

  test('a named link counts its own name, wherever it points', () => {
    const p = page(undefined, { countReady: true });
    p.click({ 'data-event': 'business-mail', href: 'mailto:business@openipc.org' });
    p.click({ 'data-event': 'download-step:business:fpv', href: '/business' });
    expect(p.counted).toEqual(['business-mail', 'download-step:business:fpv']);
  });

  test('an unnamed link to another host counts by host; links that stay count nothing', () => {
    const p = page(undefined, { countReady: true });
    p.click({ href: 'https://github.com/OpenIPC/firmware' });
    p.click({ href: 'HTTP://t.me/openipc' });
    p.click({ href: 'https://openipc.org/donate' });
    p.click({ href: '/donate' });
    p.click({ href: 'mailto:x@example.com' });
    p.click({ href: 'https://' });
    p.click(null);
    expect(p.counted).toEqual(['ext:github.com', 'ext:t.me']);
  });

  test('on a mirror, the mirror is "the site" and the origin is not', () => {
    const p = page('https://openipc.ru/ru/donate', { countReady: true });
    p.click({ href: 'https://openipc.ru/ru/' });
    p.click({ href: 'https://openipc.org/' });
    expect(p.counted).toEqual(['ext:openipc.org']);
  });

  test('a click before count.js has run is still sent, once, as an event', () => {
    // Qodo on #362: the listener used to come from a deferred module, so a
    // click while the page was still parsing was lost.
    const p = page();
    p.click({ 'data-event': 'oc-checkout', href: 'https://opencollective.com/openipc' });
    expect(p.counted).toEqual([]);
    expect(p.beacons).toHaveLength(1);
    expect(beaconName(p.beacons[0])).toBe('oc-checkout');
    expect(p.beacons[0]).toContain('e=true');
    expect(p.beacons[0].startsWith('/api/a/count?')).toBe(true);
  });
});

describe('the landing tag', () => {
  test('is stripped at once and counted when count.js is ready', () => {
    const p = page('https://openipc.org/donate?ref=tg-ru&x=1#a');
    p.landing();
    expect(p.address).toBe('https://openipc.org/donate?x=1#a');
    p.countJsRuns();
    p.fire('DOMContentLoaded');
    p.fire('pagehide');
    expect(p.counted).toEqual(['ref:tg-ru']);
  });

  test('a reader who leaves before the document is ready still sends it', () => {
    // Qodo on #362: it used to wait for `load`, which a quick exit never sees.
    const p = page('https://openipc.org/?ref=readme');
    p.landing();
    p.fire('pagehide');
    expect(p.beacons.map(beaconName)).toEqual(['ref:readme']);
  });

  test('a malformed tag is stripped and never recorded', () => {
    const p = page(`https://openipc.org/?ref=${encodeURIComponent('<script>x</script>')}`, { countReady: true });
    p.landing();
    p.fire('DOMContentLoaded');
    expect(p.address).toBe('https://openipc.org/');
    expect(p.counted).toEqual([]);
  });

  test('a page the home redirect is leaving keeps the tag for the next one', () => {
    const p = page('https://openipc.org/?ref=tg-ru', { countReady: true });
    p.window.openipcLeaving = true;
    p.landing();
    p.fire('DOMContentLoaded');
    expect(p.address).toBe('https://openipc.org/?ref=tg-ru');
    expect(p.counted).toEqual([]);
  });
});

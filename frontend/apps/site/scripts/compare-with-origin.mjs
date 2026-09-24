/**
 * Hold a built page next to the Rails page it replaces (#160).
 *
 *   node scripts/compare-with-origin.mjs <dist> <path> [width] [options]
 *     --shots <dir>    write a.png (origin) and b.png (bundle), full page
 *     --probe <file>   run a function exported by <file> on both, print both
 *     --origin <url>   default https://openipc.org
 *
 * The bundle has one hard requirement: a visitor cannot tell which half of the
 * site they are on. Reading the DOM does not establish that -- both pages are
 * valid, neither scrolls, nothing 404s, and they still do not look alike. This
 * serves the built bundle and drives it and the origin through the same
 * browser at the same width, so the difference that remains is the page's.
 *
 * Three things it does that a naive capture does not, each of which produced a
 * wrong answer first:
 *
 *   * serves /fonts/ from the origin. The bundle carries no font files --
 *     nginx falls through to Rails for them -- so a dist-only server renders
 *     in a fallback face and every width it reports is wrong. The lede
 *     measured 763px against the origin's 720 for exactly this reason.
 *   * neutralises `position: sticky`. A full-page screenshot smears a sticky
 *     header down the middle of the image, which is the single largest false
 *     difference between these two pages.
 *   * emulates hover and a fine pointer. Headless Chromium reports
 *     `(hover: hover)` as false, so every `hover:` rule is behind a media
 *     query that never matches and menus never open.
 *
 * Needs a Chromium, which is why it runs in the puppeteer image:
 *
 *   docker run --rm -u $(id -u):$(id -g) -e HOME=/tmp \
 *     -e PUPPETEER_CACHE_DIR=/home/pptruser/.cache/puppeteer \
 *     -v "$PWD/../../..":/w -v /tmp/shots:/out \
 *     ghcr.io/puppeteer/puppeteer:latest bash -c \
 *     'mkdir -p /tmp/s && ln -s /home/pptruser/node_modules /tmp/s/ && \
 *      cp /w/frontend/apps/site/scripts/compare-with-origin.mjs /tmp/s/ && cd /tmp/s && \
 *      node compare-with-origin.mjs /w/frontend/apps/site/dist /get-started 1440 --shots /out'
 *
 * The pictures are then compared with scripts/diff-png.py, which prints the
 * bands of rows that differ -- the useful output, because "2% of pixels" says
 * nothing about where and a band at row 930 sends you straight to it.
 */
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { extname, join, normalize } from 'node:path';
import puppeteer from 'puppeteer';

const argv = process.argv.slice(2);
const [dist, path, widthArg] = argv.filter((a) => !a.startsWith('--') && !isOption(argv, a));
function isOption(all, value) {
  const i = all.indexOf(value);
  return i > 0 && all[i - 1]?.startsWith('--');
}
function option(name, fallback) {
  const i = argv.indexOf(`--${name}`);
  return i === -1 ? fallback : argv[i + 1];
}

if (!dist || !path) {
  console.error('usage: node scripts/compare-with-origin.mjs <dist> <path> [width] [--shots <dir>] [--probe <file>] [--origin <url>]');
  process.exit(2);
}

const width = Number(widthArg ?? 1440);
const origin = option('origin', 'https://openipc.org');
const shots = option('shots');
const probeFile = option('probe');
const probe = probeFile ? await readFile(probeFile, 'utf8') : null;

const TYPES = {
  '.html': 'text/html', '.css': 'text/css', '.js': 'text/javascript',
  '.svg': 'image/svg+xml', '.webp': 'image/webp', '.png': 'image/png',
  '.jpg': 'image/jpeg', '.woff2': 'font/woff2', '.json': 'application/json',
};

const server = createServer(async (request, response) => {
  const url = new URL(request.url, 'http://local');

  let file = join(dist, normalize(url.pathname));
  if (!extname(file)) file = join(file, 'index.html');

  // Anything the bundle does not carry comes from the origin -- which is the
  // seam itself: nginx answers from the bundle when the file is there and
  // falls through to Rails when it is not. The fonts live in Rails' public/
  // and the backer count is an endpoint, so a dist-only server rendered this
  // page in a fallback face with the count missing, and reported a page 218px
  // shorter than the origin's for reasons that were the harness, not the page.
  if (!existsSync(file)) {
    const upstream = await fetch(origin + url.pathname + url.search);
    const body = Buffer.from(await upstream.arrayBuffer());
    response.writeHead(upstream.status, {
      'content-type': upstream.headers.get('content-type') ?? 'application/octet-stream',
    });
    response.end(body);
    return;
  }
  response.writeHead(200, { 'content-type': TYPES[extname(file)] ?? 'application/octet-stream' });
  response.end(await readFile(file));
});

await new Promise((r) => server.listen(0, '127.0.0.1', r));
const local = `http://127.0.0.1:${server.address().port}`;

const browser = await puppeteer.launch({ args: ['--no-sandbox'] });

try {
  for (const [host, name, label] of [[origin, 'a', 'origin'], [local, 'b', 'bundle']]) {
    const page = await browser.newPage();

    const cdp = await page.createCDPSession();
    await cdp.send('Emulation.setEmulatedMedia', {
      features: [{ name: 'hover', value: 'hover' }, { name: 'pointer', value: 'fine' }],
    });

    await page.setViewport({
      width, height: 1000, deviceScaleFactor: 1,
      isMobile: width < 992, hasTouch: width < 992,
    });
    await page.goto(host + path, { waitUntil: 'networkidle0', timeout: 60000 });
    if (shots) await page.addStyleTag({ content: 'nav.navbar,.site-nav{position:static !important}' });

    await page.evaluate(async () => {
      for (const img of document.querySelectorAll('img[loading="lazy"]')) img.loading = 'eager';
      window.scrollTo(0, document.body.scrollHeight);
      await new Promise((r) => setTimeout(r, 600));
      window.scrollTo(0, 0);
      await Promise.all([...document.images].filter((i) => !i.complete)
        .map((i) => new Promise((r) => { i.onload = r; i.onerror = r; })));
      await document.fonts.ready;
    });
    await new Promise((r) => setTimeout(r, 400));

    const height = await page.evaluate(() => document.documentElement.scrollHeight);
    console.log(`=== ${label.padEnd(6)} ${host}${path}  ${width}x${height}`);

    if (shots) await page.screenshot({ path: `${shots}/${name}.png`, fullPage: true });
    if (probe) console.log(await page.evaluate(eval(`(${probe})`)));

    await page.close();
  }
} finally {
  await browser.close();
  server.close();
}

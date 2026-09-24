/**
 * Drive every built page the way a visitor does, and report what is broken.
 *
 *   node scripts/sweep.mjs <dist-dir> [width ...]      default: 1440 768 390
 *
 * The build tests read the HTML; this opens it. The defects it is here for are
 * the ones that only exist once a browser has laid the page out, and every one
 * of them has already shipped past a green test suite on this branch:
 *
 *   * a page that scrolls sideways. A grid item's min-width is `auto`, so one
 *     long line -- a 96-character curl command -- widens its whole track and
 *     the document scrolls instead of the code block. 815px of page in a
 *     390px viewport, and invisible at desktop width.
 *   * an <img> with no src, which is a broken image to anything inspecting
 *     the page and to some browsers to the reader as well.
 *   * a link with no destination, a console error, a request that 404s.
 *
 * Run it against `dist` after a build. It serves the tree over loopback
 * because the pages reference /_astro/… by absolute path, and it needs a
 * Chromium, which lives in the puppeteer image:
 *
 *   docker run --rm -u $(id -u):$(id -g) -e PUPPETEER_CACHE_DIR=/home/pptruser/.cache/puppeteer \
 *     -v "$PWD":/w ghcr.io/puppeteer/puppeteer:latest \
 *     bash -c 'mkdir /tmp/s && ln -s /home/pptruser/node_modules /tmp/s/ && \
 *              cp /w/frontend/apps/site/scripts/sweep.mjs /tmp/s/ && \
 *              cd /tmp/s && node sweep.mjs /w/frontend/apps/site/dist'
 *
 * Exits non-zero when it finds anything, so it can gate a deploy.
 */
import { createServer } from 'node:http';
import { readFile, readdir } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { extname, join, normalize, relative } from 'node:path';
import puppeteer from 'puppeteer';

const [dist, ...widthArgs] = process.argv.slice(2);
if (!dist) {
  console.error('usage: node scripts/sweep.mjs <dist-dir> [width ...]');
  process.exit(2);
}

// 1440 is a laptop, 768 a tablet, 390 a phone. The overflow this exists to
// catch was invisible at the first and broke the page at the other two.
const WIDTHS = widthArgs.length ? widthArgs.map(Number) : [1440, 768, 390];

const TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.webp': 'image/webp',
  '.woff2': 'font/woff2',
  '.json': 'application/json',
  '.ico': 'image/x-icon',
};

/** Every address in the tree, from the index.html files it holds. */
async function addresses(dir, base = dir, found = []) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) await addresses(full, base, found);
    else if (entry.name === 'index.html') found.push('/' + relative(base, dir).replace(/\\/g, '/'));
  }
  return found.sort();
}

const server = createServer(async (req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1');
  let file = join(dist, normalize(url.pathname).replace(/^(\.\.[/\\])+/, ''));
  if (!extname(file)) file = join(file, 'index.html');
  if (!existsSync(file)) {
    res.writeHead(404).end('not found');
    return;
  }
  res.writeHead(200, { 'content-type': TYPES[extname(file)] ?? 'application/octet-stream' });
  res.end(await readFile(file));
});

await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const paths = (await addresses(dist)).filter((p) => p !== '/.');

const browser = await puppeteer.launch({ args: ['--no-sandbox'] });
const findings = [];

try {
  for (const path of paths) {
    for (const width of WIDTHS) {
      const page = await browser.newPage();
      const problems = [];
      page.on('pageerror', (e) => problems.push(`pageerror: ${String(e).slice(0, 120)}`));
      // Addresses the bundle does not own, and must not: every one of them is
      // in deploy/static/reserved-paths, served by nginx or Rails in front of
      // the bundle -- the analytics beacon, the fingerprinted favicon, and the
      // self-hosted IBM Plex faces both halves of the site share. They are
      // absent from a bare `dist` by design, so a local run would report a
      // 404 per page and bury the findings this exists for. Run it against
      // dev to exercise them.
      const notOurs = (url) => {
        const { pathname } = new URL(url);
        return pathname.startsWith('/api/')
          || pathname.startsWith('/fonts/')
          || pathname === '/favicon.png';
      };

      page.on('requestfailed', (r) => {
        if (!notOurs(r.url())) problems.push(`request failed: ${r.url().slice(0, 90)}`);
      });
      page.on('response', (r) => {
        if (r.status() >= 400 && !notOurs(r.url())) problems.push(`${r.status()} ${r.url().slice(0, 90)}`);
      });
      page.on('console', (m) => {
        // The console message for a blocked subresource carries no URL, so it
        // cannot be matched against the list above; the request handlers have
        // already reported anything that matters.
        if (m.type() === 'error' && !m.text().includes('Failed to load resource')) {
          problems.push(`console: ${m.text().slice(0, 120)}`);
        }
      });

      await page.setViewport({ width, height: 900 });
      await page.goto(base + path, { waitUntil: 'networkidle0', timeout: 60000 });

      // Lazy images never load in a viewport that never scrolls, and a
      // partner wall is mostly lazy images.
      await page.evaluate(async () => {
        for (const img of document.querySelectorAll('img[loading="lazy"]')) img.loading = 'eager';
        window.scrollTo(0, document.body.scrollHeight);
        await new Promise((r) => setTimeout(r, 400));
        window.scrollTo(0, 0);
        await Promise.all([...document.images].filter((i) => !i.complete)
          .map((i) => new Promise((r) => { i.onload = r; i.onerror = r; })));
      });

      problems.push(...await page.evaluate((w) => {
        const out = [];

        if (document.documentElement.scrollWidth > w + 1) {
          out.push(`scrolls sideways: ${document.documentElement.scrollWidth}px of page in ${w}px`);
          // Name the first element that sticks out, which is almost always
          // the one holding whatever is too wide.
          for (const el of document.querySelectorAll('main *')) {
            const rect = el.getBoundingClientRect();
            if (rect.width > 0 && rect.right > w + 2 && getComputedStyle(el).position !== 'fixed') {
              out.push(`  first past the edge by ${Math.round(rect.right - w)}px:`
                + ` <${el.tagName.toLowerCase()}> ${(el.textContent || '').trim().slice(0, 40)}`);
              break;
            }
          }
        }

        for (const img of document.images) {
          // An <img> with no src attribute at all is a placeholder waiting to
          // be filled -- the zoom viewer's, which takes its source when
          // somebody opens it. A src that is set and did not load is the
          // defect this is looking for.
          if (!img.getAttribute('src')) continue;
          if (!img.complete || img.naturalWidth === 0) {
            out.push(`broken image: ${(img.currentSrc || img.src).slice(-60)}`);
          }
        }

        for (const a of document.querySelectorAll('a')) {
          // A dropdown control is an anchor with `href="#"` and role=button --
          // Bootstrap's own markup, and what the navigation bar reproduces. It
          // is a control, not a link that forgot where it was going.
          if (a.getAttribute('role') === 'button' && a.hasAttribute('aria-expanded')) continue;

          const href = a.getAttribute('href');
          if (!href || href.trim() === '' || href === '#') {
            out.push(`link with no destination: ${(a.textContent || '').trim().slice(0, 40)}`);
          }
        }

        return out;
      }, width));

      const unique = [...new Set(problems)];
      if (unique.length) findings.push({ path, width, problems: unique });
      await page.close();
    }
  }
} finally {
  await browser.close();
  server.close();
}

console.log(`${paths.length} page(s) x ${WIDTHS.join('/')}px: ${findings.length} with findings`);
for (const f of findings) {
  console.log(`\n${f.path}  ${f.width}px`);
  for (const p of f.problems) console.log(`  ${p}`);
}

process.exit(findings.length ? 1 : 0);

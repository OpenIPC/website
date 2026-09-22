/**
 * Photograph the built site.
 *
 *   node scripts/shoot.mjs <dist-dir> <out-dir> [path ...]
 *
 * Serves `dist` over loopback -- the pages reference /_astro/… by absolute
 * path, so a file:// URL renders them unstyled -- and writes one full-page PNG
 * per address per viewport.
 *
 * Captured at deviceScaleFactor 2: these are looked at on HiDPI displays, and a
 * 1x screenshot of a page is unreadable there in the way that makes a reviewer
 * comment on the rendering instead of the design.
 *
 * Needs a Chromium. There is none on the developer's host and none in CI's Node
 * image, so run it through the puppeteer container:
 *
 *   docker run --rm -u $(id -u):$(id -g) -v "$PWD":/w -w /w \
 *     ghcr.io/puppeteer/puppeteer:latest \
 *     node scripts/shoot.mjs dist/ shots/ / /ru/ /zh/
 */
import { createServer } from 'node:http';
import { readFile, mkdir } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { extname, join, normalize } from 'node:path';
import puppeteer from 'puppeteer';

const [dist, outDir, ...paths] = process.argv.slice(2);
if (!dist || !outDir || paths.length === 0) {
  console.error('usage: node scripts/shoot.mjs <dist-dir> <out-dir> <path> [path ...]');
  process.exit(2);
}

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

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'mobile', width: 390, height: 844 },
];

const server = createServer(async (req, res) => {
  // Query strings and traversal both stripped before the path touches the
  // filesystem: this serves a build directory to a browser on the same
  // machine, but a static server that resolves ".." is a bad habit anywhere.
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

await mkdir(outDir, { recursive: true });
const browser = await puppeteer.launch({ args: ['--no-sandbox'] });

try {
  for (const path of paths) {
    for (const viewport of VIEWPORTS) {
      const page = await browser.newPage();
      await page.setViewport({ ...viewport, deviceScaleFactor: 2 });
      const response = await page.goto(base + path, { waitUntil: 'networkidle0' });
      if (!response.ok()) throw new Error(`${path} answered ${response.status()}`);

      // Lazy images below the fold never load in a headless viewport that
      // never scrolls, and they are most of what a partner wall is.
      await page.evaluate(async () => {
        for (const img of document.querySelectorAll('img[loading="lazy"]')) img.loading = 'eager';
        await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
        await Promise.all([...document.images].filter((i) => !i.complete)
          .map((i) => new Promise((r) => { i.onload = r; i.onerror = r; })));
      });

      const slug = (path.replace(/^\/|\/$/g, '') || 'home').replace(/\//g, '-');
      const file = join(outDir, `${slug}-${viewport.name}.png`);
      await page.screenshot({ path: file, fullPage: true });
      console.log(file);
      await page.close();
    }
  }
} finally {
  await browser.close();
  server.close();
}

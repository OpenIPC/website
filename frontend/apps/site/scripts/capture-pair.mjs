/**
 * Photograph the same address on two hosts, for comparing them pixel by pixel.
 *
 *   node scripts/capture-pair.mjs <out-dir> <path> <width> <a-url> <b-url>
 *
 * Written for #160's one hard requirement: a page served from the static
 * bundle has to be indistinguishable from the Rails page it replaces. Reading
 * the DOM cannot tell you that -- both pages are valid, neither scrolls,
 * nothing 404s, and they still do not look alike. Only the pixels can.
 *
 * Two things it does to both sides equally, so the comparison is of the page
 * and not of the camera:
 *
 *   * neutralises the sticky header. A full-page capture of a `position:
 *     sticky` element smears it into the middle of the image, which is the
 *     single largest false difference between these two pages.
 *   * emulates hover and a fine pointer. Headless Chromium reports
 *     `(hover: hover)` as false, so every `hover:` and `group-hover:` rule --
 *     which is how the menu opens -- is behind a media query that never
 *     matches.
 *
 * Needs a Chromium, which lives in the puppeteer image:
 *
 *   docker run --rm -u $(id -u):$(id -g) -e DEV_PW="$pw" \
 *     -e PUPPETEER_CACHE_DIR=/home/pptruser/.cache/puppeteer \
 *     -v "$PWD":/w -v /tmp/shots:/out ghcr.io/puppeteer/puppeteer:latest bash -c \
 *     'mkdir /tmp/s && ln -s /home/pptruser/node_modules /tmp/s/ && \
 *      cp /w/frontend/apps/site/scripts/capture-pair.mjs /tmp/s/ && cd /tmp/s && \
 *      node capture-pair.mjs /out /get-started 1440 https://openipc.org https://dev.openipc.org'
 */
import puppeteer from 'puppeteer';

const [outDir, path, width, ...hosts] = process.argv.slice(2);
if (!outDir || !path || !width || hosts.length !== 2) {
  console.error('usage: node scripts/capture-pair.mjs <out-dir> <path> <width> <a-url> <b-url>');
  process.exit(2);
}

const browser = await puppeteer.launch({ args: ['--no-sandbox'] });

try {
  for (const [i, host] of hosts.entries()) {
    const page = await browser.newPage();

    // dev.openipc.org is behind basic auth; the origin is not.
    if (host.includes('dev.') && process.env.DEV_PW) {
      await page.authenticate({ username: 'openipc', password: process.env.DEV_PW });
    }

    const cdp = await page.createCDPSession();
    await cdp.send('Emulation.setEmulatedMedia', {
      features: [
        { name: 'hover', value: 'hover' },
        { name: 'pointer', value: 'fine' },
      ],
    });

    await page.setViewport({ width: Number(width), height: 1000, deviceScaleFactor: 1 });
    await page.goto(host + path, { waitUntil: 'networkidle0', timeout: 60000 });

    await page.addStyleTag({ content: 'nav.navbar,.site-header{position:static !important}' });

    // Lazy images never load in a viewport that never scrolls, and the fonts
    // have to be in before anything is measured against them.
    await page.evaluate(async () => {
      for (const img of document.querySelectorAll('img[loading="lazy"]')) img.loading = 'eager';
      window.scrollTo(0, document.body.scrollHeight);
      await new Promise((r) => setTimeout(r, 600));
      window.scrollTo(0, 0);
      await Promise.all([...document.images].filter((x) => !x.complete)
        .map((x) => new Promise((r) => { x.onload = r; x.onerror = r; })));
      await document.fonts.ready;
    });
    await new Promise((r) => setTimeout(r, 400));

    const height = await page.evaluate(() => document.documentElement.scrollHeight);
    const name = i === 0 ? 'a' : 'b';
    await page.screenshot({ path: `${outDir}/${name}.png`, fullPage: true });
    console.log(`${name}  ${host}${path}  ${width}x${height}`);
    await page.close();
  }
} finally {
  await browser.close();
}

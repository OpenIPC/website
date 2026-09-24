/**
 * Click through the site the way a visitor does, across the seam (#160).
 *
 *   DEV_PW=... node scripts/walk-the-seam.mjs https://dev.openipc.org
 *
 * Every other check in this directory loads one page in a fresh browser, and
 * there is a class of defect that cannot be seen that way -- the one this was
 * written for. The Rails half ships Turbo Drive: it intercepted a link into
 * the bundle, swapped the body and merged the heads, so Bootstrap stayed
 * loaded and Tailwind arrived beside it. Clicking back left Tailwind's
 * preflight applied to a Rails page, where `img { height: auto }` overrides
 * the logo's height attribute -- the bar went 60.4px to 90.6, the logo 32px to
 * 64. Reported twice by somebody using the site while every single-page check
 * reported that dev matched production exactly, because the cause was the
 * state the previous navigation left behind.
 *
 * So this walks: Rails page, bundle page, Rails page, and back, clicking the
 * links rather than fetching the addresses, and asserts on each step that the
 * page carries only its own stylesheet and that the navigation bar is the size
 * it should be. Exits non-zero on the first page that is wrong.
 */
import puppeteer from 'puppeteer';

const host = process.argv[2] ?? 'https://dev.openipc.org';

// Crossings in both directions, and twice over the same page, because the
// defect this catches only appears on the second visit.
const WALK = [
  '/open-wall', '/get-started', '/supported-hardware/featured',
  '/cameras/vendors/goke/socs/gk7205v300', '/donate', '/open-wall',
  '/cameras/vendors/sigmastar', '/', '/community',
  // Straight into the wizard from a Rails page and straight back out again
  // (#164). It is the deepest page the bundle claims and the newest crossing,
  // and the walk above only ever reached it from another bundle page.
  '/open-wall', '/cameras/vendors/goke/socs/gk7205v200', '/open-wall',
  '/', '/cameras/vendors/hisilicon/socs/hi3516ev300', '/',
];

const NAV_HEIGHT = 60.4;
const LOGO_HEIGHT = 32;

const browser = await puppeteer.launch({ args: ['--no-sandbox'] });
const page = await browser.newPage();
if (host.includes('dev.') && process.env.DEV_PW) {
  await page.authenticate({ username: 'openipc', password: process.env.DEV_PW });
}
await page.setViewport({ width: 1440, height: 900, deviceScaleFactor: 1 });

let wrong = 0;
await page.goto(host + WALK[0], { waitUntil: 'networkidle0', timeout: 60000 });

for (const next of WALK.slice(1)) {
  // Clicked, not fetched: a fetch starts a fresh document and cannot carry the
  // previous page's state, which is the whole point.
  const clicked = await page.evaluate((href) => {
    const anchor = [...document.querySelectorAll('a')].find((a) => a.getAttribute('href') === href);
    if (!anchor) return false;
    anchor.click();
    return true;
  }, next);
  if (!clicked) await page.goto(host + next, { waitUntil: 'networkidle0', timeout: 60000 });
  await new Promise((r) => setTimeout(r, 1800));

  const seen = await page.evaluate(() => {
    const rails = document.querySelector('nav.navbar');
    const bundle = document.querySelector('nav.site-nav');
    const nav = rails || bundle;
    const logo = nav?.querySelector('img');
    const sheets = [...document.styleSheets].map((s) => (s.href || '').split('/').pop()).filter(Boolean);

    return {
      url: location.pathname,
      side: rails ? 'rails' : bundle ? 'bundle' : 'none',
      navHeight: nav ? +nav.getBoundingClientRect().height.toFixed(1) : 0,
      logoHeight: logo ? +logo.getBoundingClientRect().height.toFixed(1) : 0,
      links: nav ? [...nav.querySelectorAll('a')].filter((a) => a.offsetParent !== null).length : 0,
      bootstrap: sheets.some((name) => name.startsWith('application-')),
      bundleCss: sheets.some((name) => !name.startsWith('application-') && name.endsWith('.css')),
    };
  });

  const problems = [];
  if (seen.side === 'none') problems.push('no navigation at all');
  if (seen.bootstrap && seen.bundleCss) problems.push('both stylesheets are loaded');
  if (seen.navHeight !== NAV_HEIGHT) problems.push(`bar is ${seen.navHeight}px, not ${NAV_HEIGHT}`);
  if (seen.logoHeight !== LOGO_HEIGHT) problems.push(`logo is ${seen.logoHeight}px, not ${LOGO_HEIGHT}`);
  if (seen.links === 0) problems.push('no visible navigation links');

  if (problems.length > 0) wrong += 1;
  console.log(`${problems.length ? 'BAD ' : 'ok  '} ${seen.url.padEnd(40)} ${seen.side.padEnd(7)}`
    + ` bar=${seen.navHeight} logo=${seen.logoHeight} links=${seen.links}`
    + (problems.length ? `\n     ${problems.join('; ')}` : ''));
}

await browser.close();

console.log(wrong === 0
  ? `every crossing clean (${WALK.length - 1} navigations)`
  : `${wrong} page(s) wrong`);
process.exit(wrong === 0 ? 0 : 1);

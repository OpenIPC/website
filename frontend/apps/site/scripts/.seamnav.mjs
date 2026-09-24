import puppeteer from 'puppeteer';
const b = await puppeteer.launch({ args: ['--no-sandbox'] });
const p = await b.newPage();
await p.authenticate({ username: 'openipc', password: process.env.DEV_PW });
await p.setViewport({ width: 1440, height: 900, deviceScaleFactor: 1 });

const look = async (label) => {
  const m = await p.evaluate(() => {
    const rails = document.querySelector('nav.navbar');
    const astro = document.querySelector('nav.site-nav');
    const nav = rails || astro;
    const img = nav?.querySelector('img');
    const links = nav ? [...nav.querySelectorAll('a')].filter((a) => a.offsetParent !== null).length : 0;
    return {
      url: location.pathname,
      which: rails ? 'rails-navbar' : astro ? 'astro-navbar' : 'NONE',
      navH: nav ? +nav.getBoundingClientRect().height.toFixed(1) : 0,
      logoH: img ? +img.getBoundingClientRect().height.toFixed(1) : 'none',
      visibleLinks: links,
      sheets: [...document.styleSheets].map((s) => (s.href || 'inline').split('/').pop().slice(0, 28)),
      turbo: typeof window.Turbo !== 'undefined',
    };
  });
  console.log(`${label.padEnd(34)} ${m.url.padEnd(30)} ${m.which} navH=${m.navH} logo=${m.logoH} links=${m.visibleLinks} turbo=${m.turbo}`);
  console.log(`${' '.repeat(34)} sheets: ${m.sheets.join(', ')}`);
};

await p.goto('https://dev.openipc.org/open-wall', { waitUntil: 'networkidle0', timeout: 60000 });
await look('1. landed on /open-wall (rails)');

// Click the navbar's "Get Started" -- a bundle page -- the way a visitor does.
const clicked = await p.evaluate(() => {
  const a = [...document.querySelectorAll('nav.navbar a')].find((x) => x.getAttribute('href') === '/get-started');
  if (!a) return false;
  a.click();
  return true;
});
console.log('   clicked Get Started:', clicked);
await new Promise((r) => setTimeout(r, 2500));
await look('2. after clicking into bundle');

// And back again.
await p.evaluate(() => {
  const a = [...document.querySelectorAll('a')].find((x) => x.getAttribute('href') === '/open-wall');
  if (a) a.click();
});
await new Promise((r) => setTimeout(r, 2500));
await look('3. back to /open-wall');

await b.close();

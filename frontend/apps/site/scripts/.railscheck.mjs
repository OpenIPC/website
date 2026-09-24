import puppeteer from 'puppeteer';
const PATHS = ['/', '/supported-hardware/featured', '/supported-hardware/full-list',
  '/cameras/socs/gk7205v300', '/open-wall'];
const b = await puppeteer.launch({ args: ['--no-sandbox'] });
for (const path of PATHS) {
  const row = [];
  for (const host of ['https://openipc.org', 'https://dev.openipc.org']) {
    const p = await b.newPage();
    if (host.includes('dev.')) await p.authenticate({ username: 'openipc', password: process.env.DEV_PW });
    const failed = [];
    p.on('requestfailed', (r) => failed.push(r.url().slice(-40)));
    p.on('response', (r) => { if (r.status() >= 400) failed.push(r.status() + ' ' + r.url().slice(-40)); });
    await p.setViewport({ width: 1347, height: 900, deviceScaleFactor: 1 });
    let status = 0;
    try { const resp = await p.goto(host + path, { waitUntil: 'networkidle0', timeout: 45000 }); status = resp.status(); }
    catch (e) { row.push(host.replace('https://', '') + ' NAVFAIL'); await p.close(); continue; }
    const m = await p.evaluate(() => {
      const nav = document.querySelector('nav.navbar, nav.site-nav');
      if (!nav) return { nav: 'none' };
      const img = nav.querySelector('img');
      const links = [...nav.querySelectorAll('a')].filter((a) => a.offsetParent !== null).length;
      const toggler = document.querySelector('.navbar-toggler, .site-nav-toggler');
      return {
        navH: +nav.getBoundingClientRect().height.toFixed(1),
        logoH: img ? +img.getBoundingClientRect().height.toFixed(1) : 'no img',
        visibleNavLinks: links,
        togglerShown: toggler ? getComputedStyle(toggler).display !== 'none' : 'absent',
        bodyBg: getComputedStyle(document.body).backgroundColor,
        sheets: document.styleSheets.length,
      };
    });
    row.push(`${host.replace('https://openipc.org','PROD').replace('https://dev.openipc.org','DEV ')} ${status} navH=${m.navH} logo=${m.logoH} links=${m.visibleNavLinks} toggler=${m.togglerShown} sheets=${m.sheets} bg=${m.bodyBg}${failed.length ? ' FAILED:' + failed.slice(0,2).join(',') : ''}`);
    await p.close();
  }
  console.log('### ' + path);
  for (const r of row) console.log('   ' + r);
}
await b.close();

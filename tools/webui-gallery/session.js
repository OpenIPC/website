// Getting into the WebUI, staying in it, and opening a page once you are.
//
// Shared by shoot.js and verify.js so that the pages being checked are opened
// exactly the way the pages being photographed were.
const { chromium } = require('playwright');

async function launch() {
  const browser = await chromium.launch({
    // Branded Chrome, not Chromium: see README, "Codecs".
    channel: 'chrome',
    args: ['--autoplay-policy=no-user-gesture-required', '--no-sandbox'],
  });
  const ctx = await browser.newContext({
    viewport: { width: 1280, height: 872 },
    deviceScaleFactor: 2,          // 2560x1744, sharp on a Retina display
    colorScheme: 'dark',
  });
  return { browser, ctx };
}

// The camera runs a small single-threaded httpd on a device with 128 MB of RAM.
// It refuses connections for a second or two under load -- opening a dozen
// pages in a row is load, by its standards -- and a run that dies on the first
// refusal wastes the whole capture. Retry, slowly, and only then give up.
async function goto(page, url, attempts = 4) {
  for (let i = 1; ; i++) {
    try {
      return await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30000 });
    } catch (e) {
      if (i >= attempts) throw e;
      console.log(`     ${url} did not answer (${e.message.split('\n')[0]}), retry ${i}/${attempts - 1}`);
      await page.waitForTimeout(3000 * i);
    }
  }
}

async function connect({ base, user, pass }) {
  const { browser, ctx } = await launch();

  // The WebUI redirects unauthenticated *browsers* to /login.html rather than
  // answering 401, so Playwright's httpCredentials never gets a challenge to
  // respond to. Sign in through the form and the cookie carries the run.
  const signIn = async () => {
    const page = await ctx.newPage();
    await goto(page, `${base}/login.html`);
    await page.fill('#username', user);
    await page.fill('#password', pass);
    await page.click('#submit');
    await page.waitForURL(/status\.cgi/, { timeout: 20000 });
    return page;
  };

  // Open one WebUI page and let it finish assembling itself. The settle is not
  // padding: the log viewer, the settings form and the player all build their
  // content from calls made after load, and photographing before they finish
  // gives you a picture of a spinner.
  //
  // The camera can drop the session in the middle of a run -- its httpd
  // restarts under load -- and it does so by *serving the sign-in form* with a
  // 200 rather than by failing. Every page then captures as "Sign in", the run
  // reports twelve successes, the redaction check passes because a login form
  // has nothing to hide, and a gallery of sign-in forms gets installed. So:
  // never accept a page that is not the page that was asked for.
  const open = async (cgi, settle) => {
    const want = `/cgi-bin/${cgi}`;
    const page = await ctx.newPage();
    try {
      await goto(page, `${base}${want}`);
      if (new URL(page.url()).pathname !== want) {
        const fresh = await signIn();
        await fresh.close();
        await goto(page, `${base}${want}`);
      }
      const landed = new URL(page.url()).pathname;
      if (landed !== want) throw new Error(`${cgi} answered with ${landed} -- not signed in`);
      return await settled(page, settle);
    } catch (e) {
      if (!page.isClosed()) await page.close();
      throw e;
    }
  };

  const home = await signIn();
  return { ctx, home, open, close: () => browser.close() };
}

async function settled(page, settle) {
  try { await page.waitForLoadState('networkidle', { timeout: 8000 }); } catch { /* slow page */ }
  await page.waitForTimeout(settle);
  return page;
}

module.exports = { connect };

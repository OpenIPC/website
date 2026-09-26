/**
 * Drive one page the way a visitor does, and say what happened (#164).
 *
 *   DEV_PW=... node scripts/drive.mjs https://dev.openipc.org/path probe.mjs
 *
 * `compare-with-origin.mjs` holds two pages next to each other and cannot do
 * this: a probe that presses a button that navigates loses the execution
 * context it was running in. What an island does after a
 * click is exactly what a screenshot of either page cannot show, and the one
 * bug this bundle shipped twice was invisible to every check that loaded a
 * page and looked at it.
 *
 * The probe is a module-less function literal, as compare-with-origin.mjs
 * takes: it may be async, and whatever it returns is printed. Console errors
 * and failed requests are printed whether it asks for them or not, because a
 * page that renders nothing renders nothing for a reason.
 */
import { readFile } from 'node:fs/promises';
import puppeteer from 'puppeteer';

const [url, probeFile, widthArg] = process.argv.slice(2);
if (!url || !probeFile) {
  console.error('usage: node scripts/drive.mjs <url> <probe.mjs> [width]');
  process.exit(2);
}

const width = Number(widthArg ?? 1440);
const probe = await readFile(probeFile, 'utf8');

const browser = await puppeteer.launch({ args: ['--no-sandbox'] });
const page = await browser.newPage();
if (url.includes('dev.') && process.env.DEV_PW) {
  await page.authenticate({ username: 'openipc', password: process.env.DEV_PW });
}

const noise = [];
page.on('pageerror', (error) => noise.push(`[error] ${error.message}`));
page.on('console', (message) => {
  if (message.type() === 'error' || message.type() === 'warning') {
    noise.push(`[${message.type()}] ${message.text()}`);
  }
});
page.on('requestfailed', (request) => noise.push(`[failed] ${request.url()}`));

const cdp = await page.createCDPSession();
await cdp.send('Emulation.setEmulatedMedia', {
  features: [{ name: 'hover', value: 'hover' }, { name: 'pointer', value: 'fine' }],
});
await page.setViewport({ width, height: 1000, deviceScaleFactor: 1, isMobile: width < 992 });

await page.goto(url, { waitUntil: 'networkidle0', timeout: 60000 });

try {
  console.log(await page.evaluate(eval(`(${probe})`)));
} catch (error) {
  // A probe that presses a submit that navigates loses the context
  // it was running in goes with the old document. That is an answer rather
  // than a failure -- it is the difference being measured -- so say what the
  // browser ended up on instead of a stack trace.
  if (!/Execution context was destroyed/.test(String(error))) throw error;
  await page.waitForNavigation({ waitUntil: 'networkidle0', timeout: 30000 }).catch(() => {});
  console.log(`navigated away: ${page.url()}`);
}

if (noise.length > 0) console.log(`\n${noise.join('\n')}`);
await browser.close();
process.exit(noise.some((line) => line.startsWith('[error]')) ? 1 : 0);

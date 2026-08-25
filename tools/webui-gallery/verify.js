// Proves the redaction worked, by asking the opposite question.
//
// shoot.js rewrites the DOM and then photographs it, so the only record of what
// was actually published is a PNG that nothing can grep. This re-opens every
// page in the manifest, applies the same rewriting, reads the rendered text
// back, and asserts that nothing identifying survived -- the camera's hostname,
// the domain it sits in, its address, any MAC that is not the stand-in, or any
// private address the substitution missed.
//
// It is not a substitute for looking at the captures, but it is the check that
// catches what nobody looks for: a second subnet appearing in a log line, an
// address in a form field rather than in text.
const fs = require('fs');
const dns = require('dns').promises;
const yaml = require('js-yaml');
const { redactInPage, survivors, config } = require('./redact');
const { connect } = require('./session');

const BASE = process.env.CAM_BASE;
const USER = process.env.CAM_USER || 'root';
const PASS = process.env.CAM_PASS;
const MANIFEST = process.env.MANIFEST;
const ONLY = (process.env.ONLY || '').split(',').map((s) => s.trim()).filter(Boolean);
const MAPS = (process.env.MAPS || '').split(',').filter(Boolean).map((m) => {
  const at = m.indexOf('=');
  return [m.slice(0, at), m.slice(at + 1)];
});

(async () => {
  const screens = yaml.load(fs.readFileSync(MANIFEST, 'utf8')).screens
    .filter((s) => !ONLY.length || ONLY.includes(s.slug));

  const host = new URL(BASE).hostname;
  const { address: ip } = await dns.lookup(host).catch(() => ({ address: null }));
  const cfg = config({ host, ip, maps: MAPS });

  const { home, open, close } = await connect({ base: BASE, user: USER, pass: PASS });
  await home.close();

  let leaking = 0;
  for (const { slug, cgi, settle } of screens) {
    const page = await open(cgi, settle);
    await page.evaluate(redactInPage, cfg);
    // innerText, not innerHTML: what a reader sees is what a screenshot shows,
    // and an attribute nobody renders is not in the picture.
    const text = await page.evaluate(() => document.body.innerText);
    const left = survivors(text, cfg);
    if (left.length) {
      console.log(`LEAK ${slug} (${cgi}): ${left.join('; ')}`);
      leaking++;
    }
    await page.close();
  }
  await close();

  console.log(leaking
    ? `${leaking} of ${screens.length} page(s) still carry something identifying`
    : `clean: ${screens.length} page(s), nothing identifying survives redaction`);
  process.exit(leaking ? 1 : 0);
})().catch((e) => {
  // A verifier that cannot reach the camera has not verified anything. Say so
  // rather than exiting quietly, and never let the run install on that basis.
  console.error(`verification could not run: ${e.message.split('\n')[0]}`);
  process.exit(1);
});

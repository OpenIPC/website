// Photographs the pages listed in config/webui_gallery.yml from a running
// camera. Driven by run.sh; see README.md for what to pass and why.
const fs = require('fs');
const path = require('path');
const dns = require('dns').promises;
const yaml = require('js-yaml');
const { redactInPage, readInPage, survivors, config } = require('./redact');
const { connect } = require('./session');

const BASE = required('CAM_BASE');
const USER = process.env.CAM_USER || 'root';
const PASS = required('CAM_PASS');
const MANIFEST = required('MANIFEST');
const OUT = required('OUT');
const SCENE = process.env.SCENE || '';
const ONLY = split(process.env.ONLY);
const MAPS = split(process.env.MAPS).map((m) => {
  const at = m.indexOf('=');
  if (at < 1) throw new Error(`--map wants old=new, got "${m}"`);
  return [m.slice(0, at), m.slice(at + 1)];
});

// These two act when they are *rendered*, not when something is clicked.
// fw-reset.cgi carries a <pre data-cmd> that run.cgi executes on load:
// `sysupgrade -s -n -x --web`, which wipes the overlay and reboots the camera.
// fw-restart.cgi runs reboot the same way. Neither can be photographed, and the
// refusal lives here rather than in the manifest so that adding them back is
// not one careless edit away.
const NEVER_VISIT = ['fw-reset.cgi', 'fw-restart.cgi'];

function required(name) {
  const v = process.env[name];
  if (!v) throw new Error(`${name} is not set`);
  return v;
}

function split(v) {
  return (v || '').split(',').map((s) => s.trim()).filter(Boolean);
}

// Cover each playing <video> with a still picture.
//
// Whatever the camera is aimed at ends up on the public site, and a lens
// pointed at a dark bench photographs as a smear that reads as a broken image
// rather than as video. The overlay is fitted to the element's *content* box,
// so a player that draws its own border and corner radius still frames the
// picture, and object-fit:cover crops the scene the way real video fills the
// element -- nothing is letterboxed or stretched.
async function substituteScene(page, uri) {
  return page.evaluate(async (src) => {
    const px = (s) => parseFloat(s) || 0;
    const videos = [...document.querySelectorAll('video')].filter((v) => {
      const r = v.getBoundingClientRect();
      return r.width > 80 && r.height > 45;
    });

    for (const v of videos) {
      const r = v.getBoundingClientRect();
      const cs = getComputedStyle(v);
      const bl = px(cs.borderLeftWidth), bt = px(cs.borderTopWidth);
      const brr = px(cs.borderRightWidth), bb = px(cs.borderBottomWidth);
      const radius = ['borderTopLeftRadius', 'borderTopRightRadius',
                      'borderBottomRightRadius', 'borderBottomLeftRadius']
        .map((k) => `${Math.max(0, px(cs[k]) - Math.max(bl, bt))}px`).join(' ');

      const img = document.createElement('img');
      img.alt = '';
      img.src = src;
      img.style.cssText = [
        'position:fixed',
        `left:${r.left + bl}px`, `top:${r.top + bt}px`,
        `width:${r.width - bl - brr}px`, `height:${r.height - bt - bb}px`,
        'object-fit:cover', `border-radius:${radius}`,
        'z-index:2147483000', 'pointer-events:none',
      ].join(';');
      document.body.appendChild(img);
      await img.decode();
    }
    return videos.length;
  }, uri);
}

// Which pages the camera offers that the manifest does not mention. Not a
// failure -- the gallery is a selection, not an inventory -- but it is the
// signal that the WebUI grew a page worth adding, which is the whole reason
// this tool gets run again. Compared against the entire manifest, not against
// whatever --only narrowed this run to.
async function unlisted(page, listed) {
  // The nav links are relative -- "mj-settings.cgi", not "/cgi-bin/mj-settings.cgi"
  // -- because every page already lives under /cgi-bin/.
  const hrefs = await page.$$eval('a[href*=".cgi"]', (as) => as.map((a) => a.getAttribute('href')));
  const seen = new Set(hrefs.map((h) => path.posix.basename(h.split('?')[0])));
  return [...seen].filter((cgi) => !listed.includes(cgi) && !NEVER_VISIT.includes(cgi)).sort();
}

(async () => {
  const all = yaml.load(fs.readFileSync(MANIFEST, 'utf8')).screens;
  const screens = all.filter((s) => !ONLY.length || ONLY.includes(s.slug));
  if (!screens.length) throw new Error('nothing to shoot: ONLY matched no slug in the manifest');

  const refused = screens.filter((s) => NEVER_VISIT.includes(s.cgi));
  if (refused.length) {
    throw new Error(`refusing to open ${refused.map((s) => s.cgi).join(', ')}: `
      + 'these pages act on render and would reset or reboot the camera');
  }

  const host = new URL(BASE).hostname;
  // Every family, not the first answer: a camera on both stacks renders its
  // IPv6 address on the network page even when this tool reached it over v4.
  const resolved = await dns.lookup(host, { all: true }).catch(() => []);
  const cfg = config({ host, ips: resolved.map((r) => r.address), maps: MAPS });
  console.log(`camera ${host}${cfg.ips.length ? ` (${cfg.ips.join(', ')})` : ''}`
    + ` -> ${cfg.hostname} / ${cfg.cameraIp}, ${MAPS.length} extra substitution(s)`);

  const sceneUri = SCENE ? `data:image/jpeg;base64,${fs.readFileSync(SCENE).toString('base64')}` : null;
  if (sceneUri) {
    console.log(`scene: ${SCENE}`);
  } else if (screens.some((s) => s.scene)) {
    console.log(`\n!! no scene given, so ${screens.filter((s) => s.scene).map((s) => s.slug).join(', ')}`
      + ' will publish whatever this camera is pointed at.'
      + '\n!! look at those captures before committing them.\n');
  }

  const { home, open, close } = await connect({ base: BASE, user: USER, pass: PASS });
  console.log(`signed in as ${USER}, landed on "${await home.title()}"`);
  const extra = await unlisted(home, all.map((s) => s.cgi));
  await home.close();

  fs.mkdirSync(OUT, { recursive: true });
  let failed = 0;
  for (const { slug, cgi, settle, scene: needsScene } of screens) {
    let page;
    try {
      page = await open(cgi, settle);
      const hits = await page.evaluate(redactInPage, cfg);
      const covered = sceneUri ? await substituteScene(page, sceneUri) : 0;

      // The manifest says this page shows live video. If the overlay found no
      // player, the WebUI has changed shape and the picture about to be taken
      // is the camera's own view -- which is the one outcome a scene exists to
      // prevent, and it would otherwise be installed without a word.
      if (needsScene && sceneUri && covered === 0) {
        throw new Error('no live player found to cover -- the page has changed shape');
      }

      // Checked here, against the very page that is about to be photographed,
      // rather than only on the second pass in verify.js: a page reloaded a
      // minute later is not the artifact being installed.
      const left = survivors(await page.evaluate(readInPage), cfg);
      if (left.length) throw new Error(`would publish ${left.join('; ')}`);

      await page.screenshot({ path: path.join(OUT, `${slug}.png`) });
      console.log(`ok   ${slug.padEnd(24)} ${cgi.padEnd(22)} "${await page.title()}"`
        + ` (${hits} redacted, ${covered} scene)`);
    } catch (e) {
      console.log(`FAIL ${slug.padEnd(24)} ${cgi.padEnd(22)} ${e.message.split('\n')[0]}`);
      failed++;
    }
    if (page) await page.close();
  }
  await close();

  if (extra.length) {
    console.log(`\nnot in the manifest, offered by this camera: ${extra.join(', ')}`);
    console.log('add any worth showing to config/webui_gallery.yml and run again.');
  }
  if (failed) {
    // Loudly, and before anything is installed: a gallery missing a tile is
    // more obvious than a gallery showing a stale one, but only if you notice.
    console.log(`\n${failed} page(s) failed -- nothing has been installed`);
    process.exit(1);
  }
})().catch((e) => {
  console.error(`shoot failed: ${e.message.split('\n')[0]}`);
  process.exit(1);
});

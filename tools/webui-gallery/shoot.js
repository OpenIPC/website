// Photographs the pages listed in data/webui_gallery.yml from a running
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

// Cover each player with a still picture.
//
// Whatever the camera is aimed at ends up on the public site, and a lens
// pointed at a dark bench photographs as a smear that reads as a broken image
// rather than as video. The overlay is fitted to the element's *content* box,
// so a player that draws its own border and corner radius still frames the
// picture, and object-fit:cover crops the scene the way real video fills the
// element -- nothing is letterboxed or stretched.
//
// A player is any <video>, and the WebUI's own preview media: the preview
// ladder switches between WebRTC or MSE in a <video>, a software decoder
// drawing on a <canvas> and an MJPEG <img>, all marked .mj-pv-media. Covering
// only <video> would publish the camera's view the day a page fell back to
// the canvas.
//
// The picture goes in as the player's next sibling, positioned inside the
// same container, rather than on top of the whole page: what the page draws
// over its player -- detection boxes, the region editor, the control bar --
// comes later in the DOM and still draws over the scene, as it would over
// video.
async function substituteScene(page, uri) {
  return page.evaluate(async ([src, sel]) => {
    const px = (s) => parseFloat(s) || 0;
    const players = [...document.querySelectorAll(sel)].filter((v) => {
      const r = v.getBoundingClientRect();
      const cs = getComputedStyle(v);
      return r.width > 80 && r.height > 45 && cs.display !== 'none' && cs.visibility !== 'hidden';
    });

    for (const v of players) {
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
        'position:absolute', 'left:0', 'top:0', 'margin:0', 'max-width:none',
        `width:${v.offsetWidth - bl - brr}px`, `height:${v.offsetHeight - bt - bb}px`,
        'object-fit:cover', `border-radius:${radius}`,
        // Level with the player, so being its next sibling puts the scene
        // above it and below whatever the page stacks over both.
        `z-index:${cs.zIndex}`, 'pointer-events:none',
      ].join(';');
      img.dataset.galleryCover = '1';
      v.after(img);
      v.dataset.galleryScene = '1';
      // Placed by measurement after insertion rather than from offsetLeft:
      // the picture starts at its containing block's corner, and is moved by
      // however far that corner is from the player's content box.
      const want = v.getBoundingClientRect();
      const got = img.getBoundingClientRect();
      img.style.left = `${want.left + bl - got.left}px`;
      img.style.top = `${want.top + bt - got.top}px`;
      await img.decode();
    }
    return players.length;
  }, [uri, PICTURES]);
}

// What on a WebUI page is a picture from the camera: the players' media, the
// dashboard's snapshot tile (an <img> fed by polling /image.jpg through a blob
// URL), and any image whose source is one of the camera's picture endpoints.
// The manifest's scene flag is not trusted to know which pages have one --
// the dashboard photographed the lab's room once because it did not say so.
const PICTURES = [
  'video', '.mj-pv-media', '.mj-stage-media', '.mj-still-media', '#st-prev-img',
  'img[src^="blob:"]', 'img[src*="image.jpg"]', 'img[src*="mjpeg"]', 'img[src*="image.dng"]',
].join(', ');

// Any picture left on the page that the scene did not cover: every visible
// <img>, <video> or player canvas of any size, except the WebUI's own
// artwork. Asked as "is anything still showing?", like the redaction check,
// so a player the list above does not know fails the run instead of being
// photographed.
async function uncoveredPictures(page) {
  return page.evaluate((sel) => [...document.querySelectorAll(`img, video, ${sel}`)]
    .filter((e) => {
      if (e.dataset.galleryScene || e.dataset.galleryCover) return false;
      const r = e.getBoundingClientRect();
      const cs = getComputedStyle(e);
      if (r.width <= 80 || r.height <= 45 || cs.display === 'none' || cs.visibility === 'hidden') return false;
      if (e.tagName === 'IMG') {
        const u = new URL(e.currentSrc || e.src, location.href);
        if (u.protocol === 'data:' && u.href.startsWith('data:image/svg')) return false;
        if (u.origin === location.origin && u.pathname.startsWith('/a/')) return false;
      }
      return true;
    })
    .map((e) => `${e.tagName.toLowerCase()}${e.id ? '#' + e.id : ''}`), PICTURES);
}

// Answer the page's /ws/analytics with a fixture instead of the camera.
//
// The lab camera's firmware has no object detector and its view is replaced
// anyway, so the boxes it could report would sit over nothing in the scene.
// A screen that shows detection is therefore staged, and the manifest says so
// by naming the fixture: the boxes of the people in that screen's scene, in
// the scene's own pixels, sent the way the camera sends them -- source
// `object`, class 1 (human) -- and drawn by the WebUI's own overlay. The page
// that publishes the screenshot says it is staged.
//
// The overlay places a box in main-stream pixels, through the stream geometry
// the camera reports at /api/v1/osd, whatever frame size the event names. So
// the fixture, written in the scene's pixels, is scaled to that frame here.
function analyticsFixture(file) {
  const fx = JSON.parse(fs.readFileSync(file, 'utf8'));
  return async (page) => {
    let frame = [fx.w, fx.h];
    try {
      const osd = await (await page.request.get(new URL('/api/v1/osd', BASE).href)).json();
      if (Array.isArray(osd.group) && osd.group[0] > 0 && osd.group[1] > 0) frame = osd.group;
    } catch { /* the scene's own pixels, then */ }
    const sx = frame[0] / fx.w, sy = frame[1] / fx.h;
    const event = JSON.stringify({
      src: 'object', active: true, w: frame[0], h: frame[1],
      r: fx.boxes.map(([x, y, w, h]) =>
        [Math.round(x * sx), Math.round(y * sy), Math.round(w * sx), Math.round(h * sy), 1, 80]),
    });
    await page.routeWebSocket(/\/ws\/analytics$/, (ws) => {
      const timer = setInterval(() => ws.send(event), 200);
      // Closing the page does not close a routed socket, and a live interval
      // keeps node from ever exiting: stop on whichever comes first.
      ws.onClose(() => clearInterval(timer));
      page.once('close', () => clearInterval(timer));
    });
  };
}

// Notices about this particular camera rather than about the WebUI: the
// banner a camera raises on every page after it recovered from a crash. The
// lab camera has one, kept for the maintainers to read, and a gallery of the
// interface is not the place to show it.
const CAMERA_NOTICES = '.container:has(> .alert a[href="crashlog.cgi"]) { display: none !important; }';

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

  // WHATWG keeps the brackets on an IPv6 host; the WebUI renders the address
  // without them, so strip them or the substitution never matches.
  const host = new URL(BASE).hostname.replace(/^\[|\]$/g, '');
  // Every family, not the first answer: a camera on both stacks renders its
  // IPv6 address on the network page even when this tool reached it over v4.
  const resolved = await dns.lookup(host, { all: true }).catch(() => []);

  const dataUri = (file) => `data:image/jpeg;base64,${fs.readFileSync(file).toString('base64')}`;
  const sceneUri = SCENE ? dataUri(SCENE) : null;
  if (sceneUri) {
    console.log(`scene: ${SCENE}`);
  } else if (screens.some((s) => s.scene)) {
    console.log(`\n!! no scene given, so ${screens.filter((s) => s.scene).map((s) => s.slug).join(', ')}`
      + ' will publish whatever this camera is pointed at.'
      + '\n!! look at those captures before committing them.\n');
  }

  const { home, open, firmware, close } = await connect({ base: BASE, user: USER, pass: PASS });
  console.log(`signed in as ${USER}, landed on "${await home.title()}"`);
  const fw = await firmware();
  const cfg = config({ host, ips: resolved.map((r) => r.address), maps: MAPS, firmware: fw });
  console.log(`camera ${host}${cfg.ips.length ? ` (${cfg.ips.join(', ')})` : ''}`
    + ` -> ${cfg.hostname} / ${cfg.cameraIp}, ${MAPS.length} extra substitution(s),`
    + ` firmware ${fw || 'unread'}`);
  const extra = await unlisted(home, all.map((s) => s.cgi));
  await home.close();

  fs.mkdirSync(OUT, { recursive: true });
  let failed = 0;
  for (const { slug, cgi, settle, scene, analytics, click } of screens) {
    let page;
    try {
      // `scene: true` takes the run's scene; a file name takes that picture
      // from scene/ for this screen alone, whatever --scene said -- a staged
      // screen's fixture is drawn against one particular picture.
      const needsScene = Boolean(scene);
      const uri = typeof scene === 'string' ? dataUri(path.join(__dirname, 'scene', scene)) : sceneUri;
      const prepare = analytics ? analyticsFixture(path.join(__dirname, 'scene', analytics)) : null;
      page = await open(cgi, settle, prepare);
      if (click) {
        await page.click(click, { timeout: 10000 });
        await page.waitForTimeout(settle);
      }
      await page.addStyleTag({ content: CAMERA_NOTICES });
      const hits = await page.evaluate(redactInPage, cfg);
      const covered = uri ? await substituteScene(page, uri) : 0;

      // The manifest says this page shows live video. If the overlay found no
      // player, the WebUI has changed shape and the picture about to be taken
      // is the camera's own view -- which is the one outcome a scene exists to
      // prevent, and it would otherwise be installed without a word.
      if (needsScene && uri && covered === 0) {
        throw new Error('no live player found to cover -- the page has changed shape');
      }
      // Give the overlay's repaint timer a turn to draw over the scene.
      if (covered) await page.waitForTimeout(600);

      // Checked here, against the very page that is about to be photographed,
      // rather than only on the second pass in verify.js: a page reloaded a
      // minute later is not the artifact being installed.
      const left = survivors(await page.evaluate(readInPage), cfg);
      if (left.length) throw new Error(`would publish ${left.join('; ')}`);

      // Last, immediately before the shutter: a snapshot tile that only
      // became visible after the scene went down is still the camera's view.
      if (uri) {
        const bare = await uncoveredPictures(page);
        if (bare.length) throw new Error(`camera picture left uncovered: ${bare.join(', ')}`);
      }
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
    console.log('add any worth showing to data/webui_gallery.yml and run again.');
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

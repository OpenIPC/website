// The share page: reads the link, opens the tunnel to the camera, and shows
// the camera's own interface through it (or, for a view-only link, a player).
import { Tunnel, ShareError, concat, relayToken } from './tunnel.js';
import { openWebSocket } from './websocket.js';
import { trace, detailsControl } from './diag.js';

const $ = (id) => document.getElementById(id);
const SCOPES = { view: 'can watch', admin: 'can watch and change settings', full: 'full access' };

// What the diagnostics add to their header: where the page is, and the path.
const diagExtra = () => ({ stage, share: shareId(), scope: tunnel && tunnel.welcome ? tunnel.welcome.scope : '-' });

function notice(title, text, bad) {
  const n = $('notice') || Object.assign(document.createElement('div'), { id: 'notice' });
  n.className = 'notice' + (bad ? ' bad' : '');
  n.innerHTML = '';
  if (!bad) n.append(Object.assign(document.createElement('div'), { className: 'spin' }));
  n.append(Object.assign(document.createElement('h1'), { textContent: title }));
  if (text) n.append(Object.assign(document.createElement('p'), { textContent: text }));
  // Every failure carries the page's own account of what happened.
  if (bad) {
    trace('shown to the guest', `${title}: ${text || ''}`);
    detailsControl(n, diagExtra);
  }
  $('main').replaceChildren(n);
}

// The share is the first label of the host (<id>.share.openipc.org); ?share=
// is for a developer serving the page somewhere else.
function shareId() {
  const q = new URLSearchParams(location.search).get('share');
  return q || location.hostname.split('.')[0];
}

// The secret arrives in the fragment and is moved out of the address bar at
// once, into this tab's session storage, so a reload still works and a
// screenshot or a shoulder does not carry it off.
function secretFor(id) {
  const [secret, ...opts] = location.hash.slice(1).split('&');
  const k = 'share-secret:' + id;
  if (secret) {
    sessionStorage.setItem(k, secret);
    sessionStorage.setItem(k + ':opts', opts.join('&'));
    history.replaceState(null, '', location.pathname + location.search);
  }
  return { secret: sessionStorage.getItem(k), opts: new URLSearchParams(sessionStorage.getItem(k + ':opts') || '') };
}

function countdown(expires) {
  const tick = () => {
    const s = Math.max(0, expires - Date.now() / 1000);
    const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
    $('left').textContent = 'Link expires in ' + (d ? `${d} d ${h} h` : h ? `${h} h ${m} min` : `${m} min`);
  };
  tick();
  setInterval(tick, 30000);
}

let tunnel;

// The service worker hands every camera request to this page, which is the
// one holding the tunnel.
const HOP_RESP = new Set(['connection', 'keep-alive', 'transfer-encoding', 'set-cookie', 'upgrade']);
const SHIM = new TextEncoder().encode('<script src="/__share/shim.js"></script>');

function onWorkerMessage(e) {
  if (!e.data || e.data.type !== 'fetch') return;
  const port = e.ports[0];
  if (!tunnel || tunnel.gone) { port.postMessage({ type: 'error', message: 'not connected' }); return; }
  if (e.data.html) trace('page load', e.data.path.split('?')[0]);
  const { method, path, headers, body, html } = e.data;
  let head = null;
  let parts = null;
  const get = (n) => (head.headers.find(([k]) => k.toLowerCase() === n) || [])[1];
  const s = tunnel.request({ method, path, headers, body }, {
    onHead(h) {
      head = h;
      const hdrs = h.headers.filter(([k]) => !HOP_RESP.has(k.toLowerCase()));
      const msg = { type: 'head', status: h.status, statusText: h.statusText, headers: hdrs,
        location: get('location'), gzip: /gzip/i.test(get('content-encoding') || '') };
      if (html && /text\/html/i.test(get('content-type') || '') && !msg.location) {
        parts = [];
        head.msg = msg;
        return;
      }
      port.postMessage(msg);
    },
    onBody(b) {
      if (parts) { parts.push(b); return; }
      const copy = b.slice();
      port.postMessage({ type: 'body', data: copy.buffer }, [copy.buffer]);
    },
    async onEnd() {
      if (parts) {
        const msg = head.msg;
        let bytes = concat(parts);
        if (msg.gzip) {
          bytes = new Uint8Array(await new Response(new Blob([bytes]).stream()
            .pipeThrough(new DecompressionStream('gzip'))).arrayBuffer());
          msg.gzip = false;
          msg.headers = msg.headers.filter(([k]) => k.toLowerCase() !== 'content-encoding');
        }
        msg.headers = msg.headers.filter(([k]) => k.toLowerCase() !== 'content-length');
        port.postMessage(msg);
        const out = inject(bytes);
        port.postMessage({ type: 'body', data: out.buffer }, [out.buffer]);
      }
      port.postMessage({ type: 'end' });
    },
    onError(err) {
      trace('request failed', `${method} ${path.split('?')[0]}: ${err && err.message || err}`);
      port.postMessage({ type: 'error', message: String(err && err.message || err) });
    },
  });
  port.onmessage = (m) => { if (m.data && m.data.type === 'abort' && s.abort) s.abort(); };
}

// The shim goes first in <head>, before any of the page's own scripts.
function inject(bytes) {
  const text = new TextDecoder('latin1').decode(bytes.subarray(0, Math.min(bytes.length, 4096)));
  const m = /<head[^>]*>/i.exec(text);
  const at = m ? m.index + m[0].length : 0;
  return concat([bytes.subarray(0, at), SHIM, bytes.subarray(at)]);
}

// What the page is doing, for a stall to name.
let stage = 'starting';
// Set once the start has an outcome -- connected, failed, or a link that was
// never complete. Nothing that finishes later may change what the guest saw:
// not a late connection, not the watchdog, not the worker's reload.
let settled = false;
function settle() {
  if (settled) return false;
  settled = true;
  clearTimeout(watchdog);
  return true;
}
let watchdog;

// Resolves with p, or rejects after ms with a sentence naming the stage.
function within(p, ms, what) {
  return Promise.race([p, new Promise((_, rej) => setTimeout(() => rej(new ShareError(what)), ms))]);
}

async function worker() {
  if (!('serviceWorker' in navigator)) throw new ShareError('This browser cannot open shared cameras.');
  navigator.serviceWorker.addEventListener('message', onWorkerMessage);
  await navigator.serviceWorker.register('/__share/sw.js', { scope: '/' });
  const reg = await navigator.serviceWorker.ready;
  trace('service worker', { active: reg.active ? reg.active.state : 'none', controlling: !!navigator.serviceWorker.controller });
  if (navigator.serviceWorker.controller) return;
  trace('service worker not in control; asking it to claim this page');
  // Active but not in control of this page: a hard reload, or a page that
  // loaded while the worker was being replaced. Its activation -- where it
  // claims pages -- is already over, so waiting for controllerchange alone
  // waits for ever. Ask it to claim this page; failing that, reload once,
  // which an active worker always controls.
  const changed = new Promise((r) => navigator.serviceWorker.addEventListener('controllerchange', r, { once: true }));
  if (reg.active) reg.active.postMessage({ type: 'claim' });
  try {
    await within(changed, 5000, 'worker');
  } catch (e) {
    trace('service worker did not claim the page');
    if (settled) return;
    if (!sessionStorage.getItem('share-reloaded')) {
      sessionStorage.setItem('share-reloaded', '1');
      location.reload();
      await new Promise(() => {});
    }
    throw new ShareError('This page could not take charge of the camera’s pages. Close the tab and open the link again.');
  }
}

async function main() {
  trace('page', { path: location.pathname, hasSecretInLink: location.hash.length > 1, reloadedOnce: !!sessionStorage.getItem('share-reloaded') });
  // Still connecting after 10 s: the guest can see why, and send it.
  setTimeout(() => {
    const n = $('notice');
    if (stage !== 'connected' && n && !n.classList.contains('bad') && !n.querySelector('.diag')) {
      n.append(Object.assign(document.createElement('p'), { textContent: 'This is taking longer than it should.' }));
      detailsControl(n, diagExtra);
    }
  }, 10000);
  $('diag-open').onclick = () => {
    const open = document.querySelector('.popover');
    if (open) { open.remove(); return; }
    const pop = Object.assign(document.createElement('div'), { className: 'popover' });
    detailsControl(pop, diagExtra).querySelector('button').click();
    $('main').append(pop);
  };
  const id = shareId();
  const { secret, opts } = secretFor(id);
  if (!/^[0-9a-f]{16}$/.test(id) || !secret) {
    settle();
    notice('This link is not complete', 'Ask the camera’s owner to send the whole link again.', true);
    return;
  }
  try {
    stage = 'ice';
    trace('stage', stage);
    const ctl = new AbortController();
    setTimeout(() => ctl.abort(), 8000);
    const ice = await fetch(`/__share/ice?share=${id}`, {
      signal: ctl.signal, headers: { 'X-Share-Token': await relayToken(secret) },
    }).then((r) => r.json()).catch(() => ({ iceServers: [] }));
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    tunnel = new Tunnel({
      signal: `${proto}://${location.host}/__share/signal?share=${id}`,
      share: id, secret, iceServers: ice.iceServers || [],
      policy: opts.get('relay') != null ? 'relay' : undefined,
      trace,
    });
    stage = 'worker';
    trace('stage', stage);
    await within(worker(), 15000, 'The page did not finish starting. Close the tab and open the link again.');
    sessionStorage.removeItem('share-reloaded');
    stage = 'camera';
    trace('stage', stage);
    const welcome = await tunnel.open();
    if (!settle()) {
      // The start was already declared failed; a connection that arrives
      // afterwards is closed rather than shown over the error.
      tunnel.close();
      return;
    }
    stage = 'connected';
    trace('stage', stage);
    window.__share = { openWebSocket: (path, protocols, h) => openWebSocket(tunnel, path, protocols, h), welcome };
    tunnel.onclose = (why) => {
      $('bar').hidden = true;
      notice('The camera is no longer shared with you', why, true);
    };
    $('scope').textContent = SCOPES[welcome.scope] || welcome.scope;
    countdown(welcome.expires);
    $('bar').hidden = false;
    $('leave').onclick = () => tunnel.lost('You disconnected.');
    if (welcome.scope === 'view') {
      const p = Object.assign(document.createElement('div'), { className: 'player' });
      p.append(Object.assign(document.createElement('img'), { src: '/mjpeg', alt: 'Live video' }));
      $('main').replaceChildren(p);
    } else {
      const f = document.createElement('iframe');
      f.title = 'Camera';
      f.src = location.pathname === '/' ? '/' : location.pathname + location.search;
      f.addEventListener('load', () => {
        try {
          const l = f.contentWindow.location;
          history.replaceState(null, '', l.pathname + l.search);
        } catch (e) { /* not ours to read */ }
      });
      $('main').replaceChildren(f);
    }
    window.__shareReady = welcome;
  } catch (e) {
    if (!settle()) return;
    const known = e instanceof ShareError;
    notice('Could not open the shared camera', known ? e.message : 'Something went wrong: ' + e.message, true);
    window.__shareError = e.message;
  }
}

// Nothing may leave the page spinning: an error that escaped, or a promise
// that failed with no one to hear it, is said on the page with where it
// happened, and in the console for whoever looks.
function fatal(what) {
  if (!settle()) return;
  console.error('share page stalled at', stage, what);
  trace('error', what);
  notice('Could not open the shared camera', `${what} (at: ${stage})`, true);
  window.__shareError = `${stage}: ${what}`;
}
window.addEventListener('error', (e) => fatal(e.message || 'a script error'));
window.addEventListener('unhandledrejection', (e) => fatal((e.reason && e.reason.message) || String(e.reason)));
// The whole start, bounded, and longer than its stages' own budgets put end
// to end (ICE 8 s + worker 15 s + camera 30 s), so a stage always gets to say
// its own reason first.
watchdog = setTimeout(() => fatal('Starting took too long'), 60000);

main();

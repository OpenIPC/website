// The share page's own flight recorder: what it did and when, for a guest to
// copy and send when something does not work. Kept in memory, bounded, and
// never holding the link's secret -- only the share id, which is in the host
// name anyway.
const START = performance.now();
const LINES = [];
const MAX = 300;

export function trace(what, detail) {
  const t = ((performance.now() - START) / 1000).toFixed(2).padStart(7);
  let line = `${t}s  ${what}`;
  if (detail !== undefined) line += '  ' + (typeof detail === 'string' ? detail : JSON.stringify(detail));
  LINES.push(line.slice(0, 400));
  if (LINES.length > MAX) LINES.splice(0, LINES.length - MAX);
}

// What this browser offers, as the share page needs to tell browsers apart
// that say the same thing in their user agent. Safari's Lockdown Mode takes
// away service workers, WebRTC, WebGL and IndexedDB; an app's built-in Safari
// view takes away the service worker and leaves the rest; a private window
// keeps them all.
function features() {
  const has = (f) => { try { return !!f(); } catch (e) { return false; } };
  const webgl = has(() => document.createElement('canvas').getContext('webgl'));
  return [
    ['webrtc', has(() => window.RTCPeerConnection)],
    ['mse', has(() => window.MediaSource || window.ManagedMediaSource)],
    ['webgl', webgl],
    ['indexeddb', has(() => window.indexedDB)],
    ['cacheapi', has(() => window.caches)],
    ['standalone', has(() => navigator.standalone)],
  ].map(([k, v]) => `${k}=${v ? 'yes' : 'no'}`).join(' ');
}

// Where the page's verdict comes from (shell.js sets it): the report leads
// with the same lines the failure screen shows.
let verdictSource = null;
export function setVerdict(fn) { verdictSource = fn; }

export function report(extra = {}) {
  let lead = [];
  try { if (verdictSource) lead = verdictLines(verdictSource()); } catch (e) { /* the rest still goes */ }
  const head = {
    page: location.host,
    at: new Date().toISOString(),
    browser: navigator.userAgent,
    online: navigator.onLine,
    serviceWorker: 'serviceWorker' in navigator
      ? (navigator.serviceWorker.controller ? 'controlling' : 'not controlling') : 'unsupported',
    features: features(),
    ...extra,
  };
  return ['OpenIPC share diagnostics', ...lead, ...Object.entries(head).map(([k, v]) => `${k}: ${v}`), '', ...LINES].join('\n');
}

// A "Show details" control with the report and a Copy button, appended to
// `parent`. The report is taken when it is opened, so it is current.
export function detailsControl(parent, extra) {
  const wrap = document.createElement('div');
  wrap.className = 'diag';
  const toggle = Object.assign(document.createElement('button'), { type: 'button', textContent: 'Show details' });
  const box = document.createElement('div');
  box.hidden = true;
  const pre = document.createElement('pre');
  const copy = Object.assign(document.createElement('button'), { type: 'button', textContent: 'Copy diagnostics' });
  const hint = Object.assign(document.createElement('p'), {
    textContent: 'Send this to whoever shared the camera with you. It contains no password or link secret.',
  });
  toggle.onclick = () => {
    box.hidden = !box.hidden;
    toggle.textContent = box.hidden ? 'Show details' : 'Hide details';
    if (!box.hidden) pre.textContent = report(typeof extra === 'function' ? extra() : extra);
  };
  copy.onclick = async () => {
    const text = report(typeof extra === 'function' ? extra() : extra);
    pre.textContent = text;
    try {
      await navigator.clipboard.writeText(text);
      copy.textContent = 'Copied';
    } catch (e) {
      const range = document.createRange();
      range.selectNodeContents(pre);
      getSelection().removeAllRanges();
      getSelection().addRange(range);
      copy.textContent = 'Selected: press Ctrl/Cmd+C';
    }
  };
  box.append(hint, pre, copy);
  wrap.append(toggle, box);
  parent.append(wrap);
  return wrap;
}

// ---- the verdict ----------------------------------------------------------
//
// What a guest's screenshot of a failure has to carry, in the few lines the
// failure screen shows before anything scrolls: where the start stopped, the
// path it had, the browser, and a reference that finds the session in the
// relay's log (share/session, sharerelay/relay.go outcome) -- where the
// camera, who else it was holding and what it said about itself are kept.
// `f` is Tunnel.facts() (or null before there was a tunnel); `worker` whether
// the browser offers a service worker; `stage` where the page was.

// "Chrome 151 · Android 10", "Safari 26.6 · iOS", "Firefox 157 · Windows".
export function browserName(ua) {
  ua = String(ua || '');
  const os = /Android ([\d.]+)/.exec(ua) ? 'Android ' + /Android ([\d.]+)/.exec(ua)[1]
    : /iPhone|iPad|iPod/.test(ua) ? 'iOS'
      : /Mac OS X/.test(ua) ? 'macOS' : /Windows/.test(ua) ? 'Windows' : /Linux/.test(ua) ? 'Linux' : '';
  const pick = [
    [/EdgA?\/(\d+)/, 'Edge'], [/SamsungBrowser\/(\d+)/, 'Samsung'], [/YaBrowser\/(\d+)/, 'Yandex'],
    [/CriOS\/(\d+)/, 'Chrome'], [/FxiOS\/(\d+)/, 'Firefox'], [/Firefox\/(\d+)/, 'Firefox'],
    [/Chrome\/(\d+)/, 'Chrome'], [/Version\/([\d.]+).*Safari/, 'Safari'],
  ];
  let name = 'Browser';
  for (const [re, n] of pick) {
    const m = re.exec(ua);
    if (m) { name = `${n} ${m[1]}`; break; }
  }
  if (/; wv\)/.test(ua)) name += ' (app view)';
  if (/Telegram/i.test(ua)) name += ' (Telegram)';
  return os ? `${name} · ${os}` : name;
}

const STAGE_WORDS = { starting: 'starting', ice: 'reaching the relay', camera: 'reaching the camera', connected: 'connected' };

export function verdict(f, { worker = true, noWorkerLink = false, workerFailed = false, stage = '', error = '', share = '', at = new Date(), ua = '', net = '' } = {}) {
  let where;
  const said = f && f.said;
  if (noWorkerLink) {
    where = 'this browser: it has no service worker, which a link that can change settings needs';
  } else if (workerFailed) {
    // The page's own start, not the camera's: the worker that carries the
    // camera's pages did not take charge in time.
    where = 'this browser: the page\u2019s service worker did not start';
  } else if (!f) {
    where = `this page, ${STAGE_WORDS[stage] || stage}`;
  } else if (f.relayDown && !said) {
    // The signalling stream to the relay failed -- before the relay named a
    // session or after -- and nobody had refused anything: the relay's leg.
    where = f.sid ? 'the relay: the connection to it dropped during the start' : 'the relay: this browser could not reach it';
  } else if (said && said.by === 'relay') {
    const d = said.data.toLowerCase();
    where = d.startsWith('this link is not valid') ? 'the relay: no camera holds this link (a wrong or ended link, or the camera is offline)'
      : /^(this link has expired|the owner revoked)/.test(d) ? 'the relay: the link has ended'
        : d.startsWith('too many people') ? 'the relay: the link is at its limit of guests'
          : d.startsWith('the camera is not answering') ? 'the camera: online, but not answering the relay'
            : d.startsWith('the camera went offline') ? 'the camera: it went offline during the start'
              : 'the relay: it turned the link away';
  } else if (said && said.by === 'camera') {
    where = f.channel === 'open' || f.answered && (f.ice === 'connected' || f.ice === 'completed')
      ? 'the camera refused, after the encrypted link was up'
      : 'the camera refused, before connecting';
  } else if (f.sid && !f.answered) {
    where = 'the camera: it never answered the offer';
  } else if (f.answered && !(f.ice === 'connected' || f.ice === 'completed') && f.channel !== 'open') {
    where = `the network: no path to the camera (ICE ${f.ice})`;
  } else if (f.channel === 'open' && !f.welcome) {
    where = 'the camera: the link opened but it never finished the handshake';
  } else {
    where = `${STAGE_WORDS[stage] || stage || 'the start'}`;
  }
  // The ICE summary always; the pair it settled on added when there was one,
  // so a failure after the path was up still says how far ICE got.
  const path = !f ? '-' : [
    `ICE ${f.ice}`,
    `this end: ${f.local.join('/') || 'none'}`,
    `camera: ${f.remote.join('/') || 'none'}`,
    f.turn ? (f.relayOnly ? 'TURN only' : 'TURN offered') : 'no TURN',
    f.path ? `pair ${f.path}` : '',
  ].filter(Boolean).join(' · ');
  const hhmmss = at.toISOString().slice(11, 19) + 'Z';
  return {
    where,
    why: said ? said.data : error,
    path: net ? `${path} · ${net}` : path,
    browser: browserName(ua) + (worker ? '' : ' · no service worker'),
    ref: `${share || '-'}/${(f && f.sid) || '-'} · ${hhmmss}`,
  };
}

// The verdict as the lines that lead a copied report.
export function verdictLines(v) {
  return [`stopped at: ${v.where}`, v.why ? `why: ${v.why}` : null, `path: ${v.path}`,
    `browser: ${v.browser}`, `ref: ${v.ref}`].filter(Boolean);
}

// The network type where the browser says (Chrome on Android): "4g", "wifi".
export function networkType() {
  try {
    const c = navigator.connection;
    if (!c) return '';
    return [c.type && c.type !== 'unknown' ? c.type : '', c.effectiveType || ''].filter(Boolean).join(' ');
  } catch (e) { return ''; }
}

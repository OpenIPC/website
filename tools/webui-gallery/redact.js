// What must not reach the public site, and how it is removed.
//
// The camera these screenshots come from is a real device on a real network.
// Its address, its gateway, the hostname it was reached by and its MAC are
// rendered into the status bar and into most of the pages. Publishing the MAC
// is the serious one: Snapshot is keyed on MAC and the Open Wall is public, so
// a published MAC makes that camera's snapshots findable by anyone.
//
// The rewriting and the check that the rewriting worked both live here, so they
// cannot drift apart, and so that the check can be run against the page that is
// about to be photographed rather than only against a later reload.

// Documentation values, chosen to look like an ordinary home network.
const CAMERA_IP = '192.168.1.10';
const OTHER_IP = '192.168.1.50';
const CAMERA_IP6 = '2001:db8::10';
const OTHER_IP6 = '2001:db8::50';
const MAC = '00:11:22:33:44:55';
const HOSTNAME = 'camera.local';

// Addresses the rewriting is allowed to leave alone, because it put them there.
const KEEP = [CAMERA_IP, OTHER_IP, '192.168.1.1'];

// IPv6 is handled by prefix rather than by a general literal match. A regex
// loose enough to catch every valid form also catches clock times and the MAC
// stand-in; these two prefixes are the ones a camera actually renders -- its
// own link-local address, and unique-local addresses of peers on the network.
const V6_PREFIXES = [
  /\bfe80::[0-9a-f:]*[0-9a-f](?:%[0-9a-z]+)?/gi,   // link-local
  /\bf[cd][0-9a-f]{2}:[0-9a-f:]*[0-9a-f]/gi,       // unique-local, fc00::/7
];

// Runs inside the page, immediately before the shutter. Playwright serialises
// this function to source and evaluates it in the browser, so it must not
// reference anything outside itself -- every value it needs arrives in `cfg`.
function redactInPage(cfg) {
  const macRe = /\b(?:[0-9a-f]{2}:){5}[0-9a-f]{2}\b/gi;
  const ipRe = /\b(?:\d{1,3}\.){3}\d{1,3}\b/g;

  const isPrivate = (ip) => {
    const o = ip.split('.').map(Number);
    if (o.some((n) => Number.isNaN(n) || n > 255)) return false;
    if (o[0] === 10) return true;
    if (o[0] === 172 && o[1] >= 16 && o[1] <= 31) return true;
    if (o[0] === 192 && o[1] === 168) return true;
    if (o[0] === 100 && o[1] >= 64 && o[1] <= 127) return true;   // CGNAT
    if (o[0] === 169 && o[1] === 254) return true;                // link-local
    return false;
  };

  const apply = (s) => {
    let out = s;
    // Operator-supplied literal substitutions run first: they are the specific
    // ones, and the sweeps below would otherwise swallow what they target.
    for (const [from, to] of cfg.maps) out = out.split(from).join(to);
    for (const [from, to] of [[cfg.host, cfg.hostname], [cfg.domain, 'local']]) {
      if (from) out = out.replace(new RegExp(from.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi'), to);
    }
    // Every address the camera's own name resolves to, whatever family and
    // whatever range -- a device on a public address would otherwise be
    // published verbatim.
    for (const own of cfg.ips) {
      out = out.split(own).join(own.includes(':') ? cfg.cameraIp6 : cfg.cameraIp);
    }
    // MACs before IPv6: the stand-in MAC must already be in place, or a prefix
    // sweep could take a bite out of a real one.
    out = out.replace(macRe, cfg.mac);
    for (const source of cfg.v6) out = out.replace(new RegExp(source[0], source[1]), cfg.otherIp6);
    out = out.replace(ipRe, (m) => {
      if (cfg.keep.includes(m)) return m;
      return isPrivate(m) ? cfg.otherIp : m;
    });
    return out;
  };

  let hits = 0;
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  const texts = [];
  while (walker.nextNode()) texts.push(walker.currentNode);
  for (const n of texts) {
    const out = apply(n.nodeValue);
    if (out !== n.nodeValue) { n.nodeValue = out; hits++; }
  }
  // Form fields carry the address as a value rather than as text, which the
  // walk above does not see at all -- fw-network.cgi is entirely such fields.
  for (const el of document.querySelectorAll('input, textarea')) {
    const out = apply(el.value || '');
    if (out !== el.value) { el.value = out; el.setAttribute('value', out); hits++; }
  }
  return hits;
}

// Everything a screenshot of this page would show as words. Also runs inside
// the page, and also has to be self-contained.
//
// document.body.innerText alone is not it: the values shown inside inputs and
// selects are not text nodes, and fw-network.cgi is almost nothing but those.
// Auditing innerText would have exactly the blind spot that redactInPage has a
// second pass to cover.
function readInPage() {
  const parts = [document.body.innerText];
  for (const el of document.querySelectorAll('input, textarea, select')) {
    if (el.type === 'password' || el.type === 'hidden') continue;
    const box = el.getBoundingClientRect();
    if (!box.width || !box.height) continue;          // not on screen, not in the picture
    parts.push(el.value || '');
    if (el.tagName === 'SELECT' && el.selectedOptions[0]) parts.push(el.selectedOptions[0].text);
  }
  return parts.join('\n');
}

// Anything this returns is a leak: it ran on a page that has already been
// redacted. Deliberately not written in terms of the rules above -- it asks "is
// anything identifying still here", which is the question that matters, and a
// rule that silently stopped matching would still be caught.
function survivors(text, cfg) {
  const found = new Set();
  const has = (needle) => needle && text.toLowerCase().includes(needle.toLowerCase());

  if (has(cfg.host)) found.add(`hostname ${cfg.host}`);
  if (has(cfg.domain)) found.add(`domain ${cfg.domain}`);
  for (const own of cfg.ips) {
    if (text.includes(own)) found.add(`address ${own}`);
  }
  for (const mac of text.match(/\b(?:[0-9a-f]{2}:){5}[0-9a-f]{2}\b/gi) || []) {
    if (mac.toLowerCase() !== cfg.mac.toLowerCase()) found.add(`MAC ${mac}`);
  }
  for (const [source, flags] of cfg.v6) {
    for (const v6 of text.match(new RegExp(source, flags)) || []) found.add(`IPv6 ${v6}`);
  }
  for (const ip of text.match(/\b(?:\d{1,3}\.){3}\d{1,3}\b/g) || []) {
    if (cfg.keep.includes(ip) || ip.startsWith('127.')) continue;
    const o = ip.split('.').map(Number);
    const priv = o[0] === 10
      || (o[0] === 172 && o[1] >= 16 && o[1] <= 31)
      || (o[0] === 192 && o[1] === 168)
      || (o[0] === 100 && o[1] >= 64 && o[1] <= 127)
      || (o[0] === 169 && o[1] === 254);
    if (priv) found.add(`private address ${ip}`);
  }
  return [...found];
}

// The shape all of the above expect. `host` is what the operator typed, `ips`
// every address it resolved to. A camera reached by bare address has no
// hostname to strip -- treating the digits as a name would replace the address
// with camera.local and lose the substitution that belongs to it.
function config({ host, ips = [], maps = [] }) {
  const looksNumeric = host ? /^\d{1,3}(\.\d{1,3}){3}$/.test(host) || host.includes(':') : false;
  const name = looksNumeric ? null : host;
  const dot = name ? name.indexOf('.') : -1;
  return {
    host: name,
    // A lab or corporate suffix is as identifying as the full name, and it
    // turns up on its own in search domains and in log lines.
    domain: dot > 0 ? name.slice(dot + 1) : null,
    ips: [...new Set([...(looksNumeric ? [host] : []), ...ips])],
    maps,
    hostname: HOSTNAME,
    cameraIp: CAMERA_IP,
    cameraIp6: CAMERA_IP6,
    otherIp: OTHER_IP,
    otherIp6: OTHER_IP6,
    mac: MAC,
    keep: KEEP,
    v6: V6_PREFIXES.map((re) => [re.source, re.flags]),
  };
}

module.exports = { redactInPage, readInPage, survivors, config };

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
// TEST-NET-3, the range RFC 5737 sets aside for documentation: a public
// address that belongs to nobody.
const PUBLIC_IP = '203.0.113.10';
const CAMERA_IP6 = '2001:db8::10';
const OTHER_IP6 = '2001:db8::50';
const MAC = '00:11:22:33:44:55';
const HOSTNAME = 'camera.local';

// Addresses the rewriting is allowed to leave alone, because it put them there.
const KEEP = [CAMERA_IP, OTHER_IP, PUBLIC_IP, '192.168.1.1'];

// A dotted quad followed by a build variant -- "2.6.10.11-lite" -- is the
// firmware's version, which every page prints, not an address.
const VERSION_TAIL = /^-[a-z]/i;

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
  // Colons are how this WebUI writes a MAC, but a MAC is the one thing that
  // must not get through, and other renderings exist -- hyphens, and the
  // three-group Cisco form. Over-redacting here shows up as an obviously wrong
  // string in a screenshot somebody looks at; under-redacting shows up as a
  // camera's Open Wall history.
  const macRe = /\b(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}\b|\b(?:[0-9a-f]{4}\.){2}[0-9a-f]{4}\b/gi;
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

  // Anything routable on the internet that is not reserved for a purpose that
  // identifies nobody: loopback, "this network", multicast and the
  // documentation ranges.
  const isPublic = (ip) => {
    const o = ip.split('.').map(Number);
    if (o.some((n) => Number.isNaN(n) || n > 255)) return false;
    if (isPrivate(ip) || o[0] === 0 || o[0] === 127 || o[0] >= 224) return false;
    if (o[0] === 192 && o[1] === 0 && o[2] === 2) return false;
    if (o[0] === 198 && o[1] === 51 && o[2] === 100) return false;
    if (o[0] === 203 && o[1] === 0 && o[2] === 113) return false;
    return true;
  };

  const apply = (s) => {
    let out = s;
    // Operator-supplied literal substitutions run first: they are the specific
    // ones, and the sweeps below would otherwise swallow what they target.
    for (const [from, to] of cfg.maps) out = out.split(from).join(to);
    // Anchored to host boundaries: an unanchored replacement of, say,
    // "torturelabs.com" would also rewrite it inside "nottorturelabs.com".
    for (const [from, to] of [[cfg.host, cfg.hostname], [cfg.domain, 'local']]) {
      if (!from) continue;
      const lit = from.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      out = out.replace(new RegExp(`(?<![\\w.-])${lit}(?![\\w.-])`, 'gi'), to);
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
    // Public addresses as well as private ones. WebRTC logs its reflexive
    // address -- the lab's own public IP -- and every remote candidate, which
    // is the public address of whoever was watching.
    out = out.replace(ipRe, (m, at, all) => {
      if (cfg.keep.includes(m)) return m;
      if (isPrivate(m)) return cfg.otherIp;
      if (new RegExp(cfg.versionTail[0], cfg.versionTail[1]).test(all.slice(at + m.length))) return m;
      return isPublic(m) ? cfg.publicIp : m;
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
  // Anchored the same way the replacement is. Substring matching would report
  // the stand-in as a leak of the thing it stands in for.
  const has = (needle) => {
    if (!needle) return false;
    const lit = needle.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return new RegExp(`(?<![\\w.-])${lit}(?![\\w.-])`, 'i').test(text);
  };

  if (has(cfg.host)) found.add(`hostname ${cfg.host}`);
  if (has(cfg.domain)) found.add(`domain ${cfg.domain}`);
  for (const own of cfg.ips) {
    if (text.includes(own)) found.add(`address ${own}`);
  }
  for (const mac of text.match(/\b(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}\b|\b(?:[0-9a-f]{4}\.){2}[0-9a-f]{4}\b/gi) || []) {
    if (mac.toLowerCase() !== cfg.mac.toLowerCase()) found.add(`MAC ${mac}`);
  }
  for (const [source, flags] of cfg.v6) {
    for (const v6 of text.match(new RegExp(source, flags)) || []) found.add(`IPv6 ${v6}`);
  }
  for (const hit of text.matchAll(/\b(?:\d{1,3}\.){3}\d{1,3}\b/g)) {
    const ip = hit[0];
    if (cfg.keep.includes(ip) || ip.startsWith('127.')) continue;
    const o = ip.split('.').map(Number);
    if (o.some((n) => n > 255)) continue;
    const priv = o[0] === 10
      || (o[0] === 172 && o[1] >= 16 && o[1] <= 31)
      || (o[0] === 192 && o[1] === 168)
      || (o[0] === 100 && o[1] >= 64 && o[1] <= 127)
      || (o[0] === 169 && o[1] === 254);
    if (priv) { found.add(`private address ${ip}`); continue; }
    if (VERSION_TAIL.test(text.slice(hit.index + ip.length))) continue;
    const reserved = o[0] === 0 || o[0] >= 224
      || (o[0] === 192 && o[1] === 0 && o[2] === 2)
      || (o[0] === 198 && o[1] === 51 && o[2] === 100)
      || (o[0] === 203 && o[1] === 0 && o[2] === 113);
    if (!reserved) found.add(`public address ${ip}`);
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
  const domain = dot > 0 ? name.slice(dot + 1) : null;
  // Only qualified names are rewritten, and never one that already *is* the
  // stand-in. A bare label identifies nothing -- the device's own hostname is
  // on every page the gallery publishes already -- while rewriting one would
  // eat ordinary words: a camera reached as "camera" would turn the heading
  // "Camera Preview" into "camera.local Preview", and a camera reached as
  // "camera.local" would have the audit report its own stand-in as a leak and
  // fail every capture.
  const qualified = (s) => !!s && s.includes('.') && s.toLowerCase() !== HOSTNAME;
  return {
    host: qualified(name) ? name : null,
    // A lab or corporate suffix is as identifying as the full name, and it
    // turns up on its own in search domains and in log lines. A single-label
    // suffix -- lan, home, local -- is not, and is a word a UI may well use.
    domain: qualified(domain) ? domain : null,
    ips: [...new Set([...(looksNumeric ? [host] : []), ...ips])],
    maps,
    hostname: HOSTNAME,
    cameraIp: CAMERA_IP,
    cameraIp6: CAMERA_IP6,
    otherIp: OTHER_IP,
    publicIp: PUBLIC_IP,
    versionTail: [VERSION_TAIL.source, VERSION_TAIL.flags],
    otherIp6: OTHER_IP6,
    mac: MAC,
    keep: KEEP,
    v6: V6_PREFIXES.map((re) => [re.source, re.flags]),
  };
}

module.exports = { redactInPage, readInPage, survivors, config };

// The share page's service worker: every request the camera's own pages make
// -- documents, scripts, images, API calls -- is carried to the camera over
// the page's tunnel instead of the network. The page that holds the tunnel
// (the top-level shell) does the carrying; this only hands requests to it
// and streams the answers back.
//
// Left alone: the shell itself (a top-level navigation), and /__share/*,
// which is this site's, not the camera's.
const RESERVED = '/__share/';
// What the camera sent with a validator, kept so the next load asks "still
// this?" instead of fetching it again. A worker's own Response never reaches
// the browser's HTTP cache, so without this every page the guest opened
// pulled every script through the tunnel whole: 1.1 MB for the Live page,
// ten seconds of it on a lossy 4G link. The camera answers no-cache and an
// ETag, i.e. "revalidate first", which is exactly what this does. Each share
// is its own origin, so this cache is one share's; the shell deletes it when
// the share ends.
const CACHE = 'mj-share-http';
// Set once the shell says the share ended: nothing more is kept, and the
// cache is deleted after the writes already under way (kept here) land --
// a delete that raced one would see the cache written back after it.
let ended = false;
const writes = new Set();
const NULL_BODY = new Set([101, 204, 205, 304]);
const REDIRECT = new Set([301, 302, 303, 307, 308]);
// Hop-by-hop, or the browser's to set.
const DROP_REQ = new Set(['connection', 'keep-alive', 'host', 'accept-encoding', 'cookie', 'origin', 'referer', 'upgrade']);

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));
// A page loaded without this worker in control -- a hard reload bypasses it --
// asks to be claimed rather than wait for an activation that already happened.
self.addEventListener('message', (e) => {
  if (e.data && e.data.type === 'claim') e.waitUntil(self.clients.claim());
  if (e.data && e.data.type === 'ended') {
    ended = true;
    e.waitUntil(Promise.allSettled([...writes]).then(() => caches.delete(CACHE)));
  }
});

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname.startsWith(RESERVED)) return;
  if (e.request.mode === 'navigate' && e.request.destination === 'document') return;
  e.respondWith(viaTunnel(e, e.request, url));
});

// The shells that could carry a request, focused first. A worker cannot ask
// which tab an iframe belongs to; every live shell on this origin reaches the
// same camera with the same access, so any live one serves -- and one that
// answers "not connected" is passed over for the next.
async function shells() {
  const all = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
  return all.filter((c) => c.frameType === 'top-level').sort((a, b) => b.focused - a.focused);
}

async function viaTunnel(e, req, url) {
  const body = req.method === 'GET' || req.method === 'HEAD' ? null : await req.arrayBuffer();
  const headers = {};
  for (const [k, v] of req.headers) if (!DROP_REQ.has(k)) headers[k] = v;
  // gzip is the one encoding this end can undo (DecompressionStream); the
  // browser's own list names others it cannot.
  if (req.method === 'GET') headers['accept-encoding'] = 'gzip';
  const html = req.mode === 'navigate';
  const cached = await fromCache(req, headers);
  if (cached) headers['if-none-match'] = cached.headers.get('etag');
  for (const c of await shells()) {
    const r = await ask(c, e, req, url, headers, body ? body.slice(0) : null, html, cached);
    if (r) return r;
  }
  return new Response('The shared camera is not connected.', { status: 503 });
}

// A stored answer this request may revalidate, or null. Only a plain GET: a
// request carrying its own validators or no-store is the page's business.
async function fromCache(req, headers) {
  // A range is the camera's to answer: a stored whole file is not one.
  if (ended || req.method !== 'GET' || headers['if-none-match'] || headers['if-modified-since'] ||
      req.headers.has('range') || req.headers.has('if-range') ||
      /no-store/i.test(req.headers.get('cache-control') || '') || req.cache === 'no-store') return null;
  try {
    const hit = await (await caches.open(CACHE)).match(req.url);
    return hit && hit.headers.get('etag') ? hit : null;
  } catch (e) {
    return null;
  }
}

// Whether an answer may be kept: a whole 200 with a validator, and nothing
// in its Cache-Control forbidding it.
function storable(req, status, headers) {
  if (ended || req.method !== 'GET' || status !== 200 || req.headers.has('range')) return false;
  const get = (n) => (headers.find(([k]) => k.toLowerCase() === n) || [])[1];
  return !!get('etag') && !/no-store|private/i.test(get('cache-control') || '') && !get('content-range');
}

// One shell's answer, or null when that shell has no tunnel.
function ask(c, e, req, url, headers, body, html, cached) {
  const { port1, port2 } = new MessageChannel();
  c.postMessage({ type: 'fetch', method: req.method, path: url.pathname + url.search, headers, body, html },
    body ? [port2, body] : [port2]);
  return new Promise((resolve) => {
    let controller;
    let answered = false;
    const stream = new ReadableStream({
      start(ctl) { controller = ctl; },
      cancel() { port1.postMessage({ type: 'abort' }); },
    });
    port1.onmessage = (m) => {
      const d = m.data;
      if (d.type === 'head') {
        answered = true;
        if (REDIRECT.has(d.status) && d.location) {
          port1.postMessage({ type: 'abort' });
          resolve(Response.redirect(new URL(d.location, url).href, d.status));
          return;
        }
        if (d.status === 304 && cached) {
          // Still what we hold: served from here, nothing more comes.
          resolve(cached);
          return;
        }
        let out = stream;
        let hdrs = d.headers;
        if (d.gzip) {
          out = stream.pipeThrough(new DecompressionStream('gzip'));
          hdrs = hdrs.filter(([k]) => k.toLowerCase() !== 'content-encoding' && k.toLowerCase() !== 'content-length');
        }
        const empty = NULL_BODY.has(d.status) || req.method === 'HEAD';
        if (!empty && storable(req, d.status, hdrs)) {
          // The page reads one copy while the other is written down. A
          // stream that fails part-way fails the put, and nothing is kept.
          const [page, keep] = out.tee();
          out = page;
          const init = { status: d.status, statusText: d.statusText, headers: hdrs };
          const w = caches.open(CACHE).then((c) => c.put(req.url, new Response(keep, init))).catch(() => {});
          writes.add(w);
          w.finally(() => writes.delete(w));
          e.waitUntil(w);
        }
        resolve(new Response(empty ? null : out, { status: d.status, statusText: d.statusText, headers: hdrs }));
      } else if (d.type === 'body') {
        controller.enqueue(new Uint8Array(d.data));
      } else if (d.type === 'end') {
        try { controller.close(); } catch (e) { /* cancelled already */ }
        port1.close();
      } else if (d.type === 'error') {
        if (!answered && d.message === 'not connected') { port1.close(); resolve(null); return; }
        if (!answered) resolve(new Response(d.message, { status: 502 }));
        else try { controller.error(new Error(d.message)); } catch (e) { /* done already */ }
        port1.close();
      }
    };
  });
}

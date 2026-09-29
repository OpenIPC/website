// The share page's service worker: every request the camera's own pages make
// -- documents, scripts, images, API calls -- is carried to the camera over
// the page's tunnel instead of the network. The page that holds the tunnel
// (the top-level shell) does the carrying; this only hands requests to it
// and streams the answers back.
//
// Left alone: the shell itself (a top-level navigation), and /__share/*,
// which is this site's, not the camera's.
const RESERVED = '/__share/';
const NULL_BODY = new Set([101, 204, 205, 304]);
const REDIRECT = new Set([301, 302, 303, 307, 308]);
// Hop-by-hop, or the browser's to set.
const DROP_REQ = new Set(['connection', 'keep-alive', 'host', 'accept-encoding', 'cookie', 'origin', 'referer', 'upgrade']);

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname.startsWith(RESERVED)) return;
  if (e.request.mode === 'navigate' && e.request.destination === 'document') return;
  e.respondWith(viaTunnel(e.request, url));
});

async function shell() {
  const all = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
  const tops = all.filter((c) => c.frameType === 'top-level');
  return tops.find((c) => c.focused) || tops[tops.length - 1];
}

async function viaTunnel(req, url) {
  const c = await shell();
  if (!c) return new Response('The shared camera is not connected.', { status: 503 });
  const body = req.method === 'GET' || req.method === 'HEAD' ? null : await req.arrayBuffer();
  const headers = {};
  for (const [k, v] of req.headers) if (!DROP_REQ.has(k)) headers[k] = v;
  const { port1, port2 } = new MessageChannel();
  const html = req.mode === 'navigate';
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
        let out = stream;
        let hdrs = d.headers;
        if (d.gzip) {
          out = stream.pipeThrough(new DecompressionStream('gzip'));
          hdrs = hdrs.filter(([k]) => k.toLowerCase() !== 'content-encoding' && k.toLowerCase() !== 'content-length');
        }
        const empty = NULL_BODY.has(d.status) || req.method === 'HEAD';
        resolve(new Response(empty ? null : out, { status: d.status, statusText: d.statusText, headers: hdrs }));
      } else if (d.type === 'body') {
        controller.enqueue(new Uint8Array(d.data));
      } else if (d.type === 'end') {
        try { controller.close(); } catch (e) { /* cancelled already */ }
        port1.close();
      } else if (d.type === 'error') {
        if (!answered) resolve(new Response(d.message, { status: 502 }));
        else try { controller.error(new Error(d.message)); } catch (e) { /* done already */ }
        port1.close();
      }
    };
  });
}

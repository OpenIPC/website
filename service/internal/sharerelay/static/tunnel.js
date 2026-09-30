// The page's end of a camera share: one RTCPeerConnection with one data
// channel ("mj-tunnel"), a mutual proof of the share's key, and HTTP
// connections multiplexed over the channel. The camera's end and the frame
// table are described in the camera's share documentation; the constants
// below must match it.
export const T = {
  HELLO: 1, WELCOME: 2, REFUSED: 3, BYE: 4, CHALLENGE: 5, PROOF: 6,
  OPEN: 16, DATA: 17, FIN: 18, RESET: 19, CREDIT: 20,
};
const HEADER = 8;
const CHUNK = 16 * 1024;
const WINDOW = 64 * 1024;
// Held in the data channel's own buffer before this end stops sending.
const HIGH_WATER = 1024 * 1024;
// HTTP exchanges in flight at once. The camera holds a few dozen streams in
// all, WebSockets included, and a page asks for forty things as it loads:
// the rest wait here rather than being refused there.
const MAX_REQUESTS = 16;

const enc = new TextEncoder();
const dec = new TextDecoder();

function frame(type, stream, payload) {
  const p = payload == null ? new Uint8Array(0)
    : payload instanceof Uint8Array ? payload : enc.encode(payload);
  const m = new Uint8Array(HEADER + p.length);
  m[0] = type;
  new DataView(m.buffer).setUint32(4, stream);
  m.set(p, HEADER);
  return m;
}

function hex(buf) {
  return [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, '0')).join('');
}

// "sha-256 AB:cd:..." -> "ABCD...", as the camera normalises it.
export function normaliseFingerprint(sdp) {
  const m = /a=fingerprint:\S+ ([0-9A-Fa-f:]+)/.exec(sdp || '');
  return m ? m[1].replace(/:/g, '').toUpperCase() : '';
}

export async function shareKey(secret) {
  return new Uint8Array(await crypto.subtle.digest('SHA-256', enc.encode(secret)));
}

// The token the camera registers the share with, and the relay derives its id
// from: showing it is how a page proves it holds the link, not just its host
// name, when it asks for a relay.
export async function relayToken(secret) {
  const k = await crypto.subtle.importKey('raw', await shareKey(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
  return hex(await crypto.subtle.sign('HMAC', k, enc.encode('mj-share-relay-v1')));
}

export async function proof(key, who, share, pageNonce, cameraNonce, cameraFp, pageFp) {
  const k = await crypto.subtle.importKey('raw', key, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
  const msg = `${who}|mj-share-v1|${share}|${pageNonce}|${cameraNonce}|${cameraFp}|${pageFp}`;
  return hex(await crypto.subtle.sign('HMAC', k, enc.encode(msg)));
}

export class ShareError extends Error {}

const FRAME_NAMES = { 2: 'WELCOME', 3: 'REFUSED', 4: 'BYE', 5: 'CHALLENGE' };

// "host udp", "srflx udp", "relay tcp": what a candidate is, without its address.
function candType(c) {
  const m = / typ (\w+)/.exec(c || '');
  const proto = / (udp|tcp) /i.exec(c || '');
  return (m ? m[1] : '?') + (proto ? ' ' + proto[1].toLowerCase() : '');
}

// Whether a candidate names an address no relay of ours may reach: private,
// shared, loopback and link-local IPv4, unique-local and link-local IPv6, and
// mDNS names. The relays refuse all of them as peers, so in relay-only mode a
// pair with one fails at once -- and, trickled ahead of the camera's public
// address, it can be the only pair Chrome has, which it then calls failed.
export function unreachableByRelay(c) {
  const f = (c || '').split(' ');
  const a = (f[4] || '').toLowerCase();
  if (a.endsWith('.local')) return true;
  if (a.includes(':')) return /^(fc|fd|fe[89ab])/.test(a) || a === '::1';
  const o = a.split('.').map(Number);
  if (o.length !== 4 || o.some((n) => !(n >= 0 && n <= 255))) return false;
  return o[0] === 10 || o[0] === 127 || (o[0] === 169 && o[1] === 254) ||
    (o[0] === 172 && o[1] >= 16 && o[1] <= 31) || (o[0] === 192 && o[1] === 168) ||
    (o[0] === 100 && o[1] >= 64 && o[1] <= 127);
}

export class Tunnel {
  // signal: a WebSocket URL speaking the camera's signalling protocol, the
  // relay's or the camera's own. iceServers: for the RTCPeerConnection.
  constructor({ signal, share, secret, iceServers = [], policy, trace = () => {} }) {
    Object.assign(this, { signal, share, secret, iceServers, policy, trace });
    this.streams = new Map();
    this.next = 1;
    this.waiting = []; // senders held back for the channel's buffer
    this.active = 0; // HTTP exchanges in flight
    this.queued = []; // and those waiting for one to finish
    this.onclose = null;
  }

  open(timeoutMs = 30000) {
    return new Promise((resolve, reject) => {
      let settled = false;
      const done = (err, v) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        // On success the signalling socket stays: on the camera's own
        // network the session is tied to it. The relay closes it when it
        // likes, and a connected session outlives that.
        if (err) { this.close(); reject(err); } else resolve(v);
      };
      const timer = setTimeout(() => done(new ShareError('The camera did not answer in time.')), timeoutMs);
      const cfg = { iceServers: this.iceServers };
      if (this.policy) cfg.iceTransportPolicy = this.policy;
      const pc = (this.pc = new RTCPeerConnection(cfg));
      this.trace('peer connection', { iceServers: this.iceServers.map((x) => [].concat(x.urls).join(' ')), policy: this.policy || 'all' });
      pc.oniceconnectionstatechange = () => this.trace('ice state', pc.iceConnectionState);
      pc.onicegatheringstatechange = () => this.trace('ice gathering', pc.iceGatheringState);
      const dc = (this.dc = pc.createDataChannel('mj-tunnel', { ordered: true }));
      dc.binaryType = 'arraybuffer';
      dc.bufferedAmountLowThreshold = HIGH_WATER / 2;
      dc.onbufferedamountlow = () => this.drain();
      const ws = (this.ws = new WebSocket(this.signal));
      pc.onicecandidate = (e) => {
        if (e.candidate && e.candidate.candidate) this.trace('local candidate', candType(e.candidate.candidate));
        if (e.candidate && e.candidate.candidate && ws.readyState === 1)
          ws.send(JSON.stringify({ req: 'candidate', data: e.candidate.candidate }));
      };
      pc.onconnectionstatechange = () => {
        this.trace('connection state', pc.connectionState);
        if (pc.connectionState === 'connected') this.pairInfo().then((p) => this.trace('selected pair', p));
        if (pc.connectionState === 'failed') {
          if (!settled) done(new ShareError('Could not reach the camera over the network.'));
          else this.lost('The connection to the camera was lost.');
        }
      };
      ws.onopen = async () => {
        this.trace('signalling open');
        await pc.setLocalDescription(await pc.createOffer());
        this.trace('offer sent', `${pc.localDescription.sdp.length} bytes`);
        ws.send(JSON.stringify({ req: 'offer', data: pc.localDescription.sdp }));
      };
      ws.onmessage = async (ev) => {
        let m;
        try { m = JSON.parse(ev.data); } catch (e) { this.trace('signalling: unreadable message'); return; }
        this.trace('signalling ← ' + m.reply, m.reply === 'candidate' ? candType(m.data) : m.reply === 'answer' ? `${(m.data || '').length} bytes` : m.data);
        if (m.reply === 'answer') {
          this.cameraFp = normaliseFingerprint(m.data);
          await pc.setRemoteDescription({ type: 'answer', sdp: m.data });
        } else if (m.reply === 'candidate' && m.data) {
          if (this.policy === 'relay' && unreachableByRelay(m.data)) {
            this.trace('candidate skipped', 'private address; a relay cannot reach it');
            return;
          }
          await pc.addIceCandidate({ candidate: m.data, sdpMid: m.mid || '0' }).catch(() => {});
        } else if (m.reply === 'error' || m.reply === 'busy' || m.reply === 'closed') {
          done(new ShareError(m.data || 'The camera refused the connection.'));
        }
      };
      ws.onerror = () => { this.trace('signalling error'); done(new ShareError('Could not reach the sharing service.')); };
      ws.onclose = (e) => this.trace('signalling closed', `code ${e.code}${e.reason ? ' ' + e.reason : ''}`);
      const nonce = hex(crypto.getRandomValues(new Uint8Array(16)));
      let key;
      dc.onopen = async () => {
        this.trace('data channel open');
        key = await shareKey(this.secret);
        this.pageFp = normaliseFingerprint(pc.localDescription.sdp);
        dc.send(frame(T.HELLO, 0, JSON.stringify({ share: this.share, nonce })));
      };
      dc.onclose = () => {
        this.trace('data channel closed'); if (!settled) done(new ShareError('The camera closed the connection.')); else this.lost('The camera closed the connection.'); };
      dc.onmessage = async (ev) => {
        const b = new Uint8Array(ev.data);
        const type = b[0];
        const id = new DataView(b.buffer).getUint32(4);
        const payload = b.subarray(HEADER);
        if (type < 16) this.trace('camera → ' + (FRAME_NAMES[type] || 'frame ' + type), type === T.REFUSED || type === T.BYE ? dec.decode(payload) : undefined);
        switch (type) {
          case T.CHALLENGE: {
            const c = JSON.parse(dec.decode(payload));
            const want = await proof(key, 'camera', this.share, nonce, c.nonce, this.cameraFp, this.pageFp);
            if (want !== c.proof) {
              this.trace('camera proof does not verify');
              // Not the camera that holds this share's key -- or not the
              // DTLS endpoint the answer named. Say nothing more to it.
              done(new ShareError('This link is not valid.'));
              return;
            }
            this.verified = true;
            const mine = await proof(key, 'page', this.share, nonce, c.nonce, this.cameraFp, this.pageFp);
            dc.send(frame(T.PROOF, 0, JSON.stringify({ proof: mine })));
            return;
          }
          case T.WELCOME:
            // Only after this end has checked the camera's proof: a WELCOME
            // from an endpoint that never proved the key is not the camera.
            if (!this.verified) { done(new ShareError('This link is not valid.')); return; }
            this.welcome = JSON.parse(dec.decode(payload));
            done(null, this.welcome);
            return;
          case T.REFUSED:
            done(new ShareError(dec.decode(payload) || 'This link is not valid.'));
            return;
          case T.BYE:
            this.lost(dec.decode(payload));
            return;
        }
        // Nothing but the handshake before the camera has proved itself.
        if (!this.welcome) return;
        const s = this.streams.get(id);
        if (!s) return;
        if (type === T.DATA) s.ondata(payload.slice());
        else if (type === T.CREDIT) { s.window += new DataView(b.buffer, b.byteOffset).getUint32(HEADER); s.flush(); }
        else if (type === T.FIN) { this.streams.delete(id); s.onfin(); }
        else if (type === T.RESET) { this.streams.delete(id); s.onreset(); }
      };
    });
  }

  // The nominated candidate pair, without addresses: what the path is.
  async pairInfo() {
    try {
      const stats = await this.pc.getStats();
      let pair;
      stats.forEach((r) => { if (r.type === 'transport' && r.selectedCandidatePairId) pair = stats.get(r.selectedCandidatePairId); });
      if (!pair) stats.forEach((r) => { if (r.type === 'candidate-pair' && r.nominated && r.state === 'succeeded') pair = r; });
      if (!pair) return 'none';
      const l = stats.get(pair.localCandidateId) || {}, r = stats.get(pair.remoteCandidateId) || {};
      return { local: `${l.candidateType} ${l.protocol}`, remote: `${r.candidateType} ${r.protocol}`,
        rtt_ms: pair.currentRoundTripTime != null ? Math.round(pair.currentRoundTripTime * 1000) : undefined,
        sent: pair.bytesSent, received: pair.bytesReceived };
    } catch (e) {
      return 'unavailable';
    }
  }

  lost(why) {
    this.trace('lost', why);
    if (this.gone) return;
    this.gone = why || 'The connection to the camera ended.';
    for (const s of this.streams.values()) s.onreset();
    this.streams.clear();
    this.close();
    if (this.onclose) this.onclose(this.gone);
  }

  send(bytes) {
    if (this.dc && this.dc.readyState === 'open') this.dc.send(bytes);
  }

  drain() {
    const w = this.waiting;
    this.waiting = [];
    for (const s of w) s.flush();
  }

  // A raw byte stream into the camera's web server.
  stream() {
    if (this.gone) throw new ShareError(this.gone);
    const id = this.next++;
    const t = this;
    const s = {
      id, window: WINDOW, pending: [], finPending: false,
      ondata() {}, onfin() {}, onreset() {},
      write(bytes) { if (bytes.length) this.pending.push(bytes); this.flush(); },
      flush() {
        while (this.pending.length && this.window > 0) {
          if (t.dc.bufferedAmount > HIGH_WATER) { t.waiting.push(this); return; }
          const b = this.pending[0];
          const n = Math.min(b.length, this.window, CHUNK);
          t.send(frame(T.DATA, id, b.subarray(0, n)));
          this.window -= n;
          if (n === b.length) this.pending.shift(); else this.pending[0] = b.subarray(n);
        }
        if (!this.pending.length && this.finPending) { this.finPending = false; t.send(frame(T.FIN, id)); }
      },
      end() { this.finPending = true; this.flush(); },
      reset() { t.streams.delete(id); t.send(frame(T.RESET, id)); },
    };
    this.streams.set(id, s);
    this.send(frame(T.OPEN, id));
    return s;
  }

  // One HTTP/1.1 exchange, streamed: onHead({status, statusText, headers})
  // once, then onBody(Uint8Array) per piece, then onEnd() or onError(e).
  request(req, h) {
    if (this.active >= MAX_REQUESTS) {
      const handle = { abort() { this.aborted = true; } };
      this.queued.push(() => {
        if (handle.aborted) { this.next_(); return; }
        const s = this.request(req, h);
        handle.abort = s.abort;
      });
      return handle;
    }
    this.active++;
    let released = false;
    const release = () => { if (!released) { released = true; this.active--; this.next_(); } };
    return this.exchange(req, {
      onHead: h.onHead, onBody: h.onBody,
      onEnd: () => { release(); h.onEnd(); },
      onError: (e) => { release(); h.onError(e); },
    }, release);
  }

  next_() {
    while (this.active < MAX_REQUESTS && this.queued.length) this.queued.shift()();
  }

  exchange({ method = 'GET', path, headers = {}, body }, { onHead, onBody, onEnd, onError }, release) {
    if (this.gone) { onError(new Error(this.gone)); return { abort() {} }; }
    const s = this.stream();
    let over = false;
    const end = () => { if (!over) { over = true; onEnd(); } };
    const fail = (e) => { if (!over) { over = true; onError(e); } };
    const p = new ResponseParser(onHead, onBody, end);
    s.ondata = (b) => { try { p.push(b); } catch (e) { s.reset(); fail(e); } };
    s.onfin = () => (p.finish() ? end() : fail(new Error('the camera closed the response early')));
    s.onreset = () => fail(new Error('the connection was reset'));
    s.abort = () => { if (!over) { over = true; release(); } s.reset(); };
    let req = `${method} ${path} HTTP/1.1\r\nHost: camera\r\nConnection: close\r\n`;
    for (const [k, v] of Object.entries(headers)) req += `${k}: ${v}\r\n`;
    const b = body && body.byteLength ? new Uint8Array(body) : null;
    if (b) req += `Content-Length: ${b.length}\r\n`;
    s.write(enc.encode(req + '\r\n'));
    if (b) s.write(b);
    return s;
  }

  // The whole response at once: {status, headers, body}.
  fetch(path, opts = {}) {
    return new Promise((resolve, reject) => {
      let head;
      const parts = [];
      this.request({ ...opts, path }, {
        onHead: (h) => { head = h; },
        onBody: (b) => parts.push(b),
        onEnd: () => resolve({ ...head, body: concat(parts) }),
        onError: reject,
      });
    });
  }

  close() {
    try { this.pc && this.pc.close(); } catch (e) { /* already closed */ }
    try { this.ws && this.ws.close(); } catch (e) { /* already closed */ }
  }
}

export function concat(parts) {
  const all = new Uint8Array(parts.reduce((n, c) => n + c.length, 0));
  let o = 0;
  for (const c of parts) { all.set(c, o); o += c.length; }
  return all;
}

// An incremental HTTP/1.1 response parser: head, then a body delimited by
// Content-Length, chunked encoding, or the end of the connection.
export class ResponseParser {
  constructor(onHead, onBody, onComplete) {
    Object.assign(this, { onHead, onBody, onComplete });
    this.buf = new Uint8Array(0);
    this.state = 'head';
    this.done = false;
  }

  push(b) {
    this.buf = this.buf.length ? concat([this.buf, b]) : b;
    for (;;) {
      if (this.state === 'head') {
        const i = indexOf(this.buf, '\r\n\r\n');
        if (i < 0) { if (this.buf.length > 65536) throw new Error('response head too large'); return; }
        const lines = new TextDecoder('latin1').decode(this.buf.subarray(0, i)).split('\r\n');
        this.buf = this.buf.subarray(i + 4);
        const [, status, ...reason] = lines[0].split(' ');
        const headers = [];
        for (const l of lines.slice(1)) {
          const c = l.indexOf(':');
          if (c > 0) headers.push([l.slice(0, c).trim(), l.slice(c + 1).trim()]);
        }
        const get = (n) => (headers.find(([k]) => k.toLowerCase() === n) || [])[1];
        const te = (get('transfer-encoding') || '').toLowerCase();
        const status_ = +status;
        this.onHead({ status: status_, statusText: reason.join(' '), headers });
        if (status_ === 101) { this.state = 'raw'; }
        else if (te.includes('chunked')) { this.state = 'chunk-size'; }
        else if (get('content-length') != null) { this.left = +get('content-length'); this.state = 'length'; }
        else if (status_ === 204 || status_ === 304) { this.state = 'end'; }
        else { this.state = 'close'; }
        continue;
      }
      if (this.state === 'raw' || this.state === 'close') {
        if (this.buf.length) { this.onBody(this.buf); this.buf = new Uint8Array(0); }
        return;
      }
      if (this.state === 'length') {
        const n = Math.min(this.left, this.buf.length);
        if (n) { this.onBody(this.buf.subarray(0, n)); this.buf = this.buf.subarray(n); this.left -= n; }
        if (this.left === 0) { this.state = 'end'; continue; }
        return;
      }
      if (this.state === 'chunk-size') {
        const i = indexOf(this.buf, '\r\n');
        if (i < 0) return;
        this.left = parseInt(new TextDecoder('latin1').decode(this.buf.subarray(0, i)), 16);
        this.buf = this.buf.subarray(i + 2);
        this.state = this.left ? 'chunk' : 'trailer';
        continue;
      }
      if (this.state === 'chunk') {
        const n = Math.min(this.left, this.buf.length);
        if (n) { this.onBody(this.buf.subarray(0, n)); this.buf = this.buf.subarray(n); this.left -= n; }
        if (this.left) return;
        if (this.buf.length < 2) { this.state = 'chunk-crlf'; return; }
        this.buf = this.buf.subarray(2);
        this.state = 'chunk-size';
        continue;
      }
      if (this.state === 'chunk-crlf') {
        if (this.buf.length < 2) return;
        this.buf = this.buf.subarray(2);
        this.state = 'chunk-size';
        continue;
      }
      if (this.state === 'trailer') {
        const i = indexOf(this.buf, '\r\n');
        if (i < 0) return;
        this.buf = this.buf.subarray(i + 2);
        if (i === 0) { this.state = 'end'; continue; }
        continue;
      }
      if (this.state === 'end') {
        if (!this.done) { this.done = true; this.onComplete(); }
        return;
      }
    }
  }

  // The connection closed: complete if the body was delimited by it.
  finish() {
    if (this.done) return true;
    if (this.state === 'close' || this.state === 'raw' || this.state === 'end') { this.done = true; return true; }
    return false;
  }
}

function indexOf(buf, s) {
  const n = s.length;
  outer: for (let i = 0; i + n <= buf.length; i++) {
    for (let j = 0; j < n; j++) if (buf[i + j] !== s.charCodeAt(j)) continue outer;
    return i;
  }
  return -1;
}

// A WebSocket client over a tunnel stream (RFC 6455, client side): what the
// WebSocket shim hands the camera's WebUI in place of the browser's own,
// which cannot reach the camera from here.
import { ResponseParser, concat } from './tunnel.js';

const enc = new TextEncoder();
const dec = new TextDecoder();

function b64(bytes) { return btoa(String.fromCharCode(...bytes)); }

export function openWebSocket(tunnel, path, protocols, h) {
  const s = tunnel.stream();
  const key = b64(crypto.getRandomValues(new Uint8Array(16)));
  let open = false;
  let closed = false;
  let rx = new Uint8Array(0);
  let frag = null; // [opcode, parts] of a fragmented message
  const fin = (code, reason, clean) => {
    if (closed) return;
    closed = true;
    h.onclose(code, reason, clean);
  };
  const parser = new ResponseParser(
    (head) => {
      const hv = (n) => (head.headers.find(([k]) => k.toLowerCase() === n) || [])[1] || '';
      if (head.status !== 101) {
        s.reset();
        h.onerror();
        fin(1006, `HTTP ${head.status}`, false);
        return;
      }
      open = true;
      h.onopen(hv('sec-websocket-protocol'));
    },
    (bytes) => { if (open) { rx = rx.length ? concat([rx, bytes]) : bytes.slice(); frames(); } },
    () => {},
  );
  s.ondata = (b) => { try { parser.push(b); } catch (e) { s.reset(); h.onerror(); fin(1006, '', false); } };
  s.onfin = () => fin(1006, '', false);
  s.onreset = () => { h.onerror(); fin(1006, '', false); };

  function send(opcode, payload) {
    const n = payload.length;
    const head = [0x80 | opcode];
    if (n < 126) head.push(0x80 | n);
    else if (n < 65536) head.push(0x80 | 126, n >> 8, n & 255);
    else { head.push(0x80 | 127, 0, 0, 0, 0, (n >>> 24) & 255, (n >>> 16) & 255, (n >>> 8) & 255, n & 255); }
    const mask = crypto.getRandomValues(new Uint8Array(4));
    const out = new Uint8Array(head.length + 4 + n);
    out.set(head, 0);
    out.set(mask, head.length);
    for (let i = 0; i < n; i++) out[head.length + 4 + i] = payload[i] ^ mask[i & 3];
    s.write(out);
  }

  function frames() {
    for (;;) {
      if (rx.length < 2) return;
      const b0 = rx[0], b1 = rx[1];
      let len = b1 & 127, o = 2;
      if (len === 126) { if (rx.length < 4) return; len = (rx[2] << 8) | rx[3]; o = 4; }
      else if (len === 127) { if (rx.length < 10) return; len = rx[6] * 2 ** 24 + (rx[7] << 16) + (rx[8] << 8) + rx[9]; o = 10; }
      const masked = b1 & 128;
      const mk = masked ? rx.subarray(o, o + 4) : null;
      if (masked) o += 4;
      if (rx.length < o + len) return;
      let payload = rx.slice(o, o + len);
      if (mk) for (let i = 0; i < len; i++) payload[i] ^= mk[i & 3];
      rx = rx.subarray(o + len);
      const opcode = b0 & 15, final = b0 & 128;
      if (opcode === 8) {
        const code = len >= 2 ? (payload[0] << 8) | payload[1] : 1005;
        send(8, payload.subarray(0, 2));
        s.end();
        fin(code, dec.decode(payload.subarray(2)), true);
        return;
      }
      if (opcode === 9) { send(10, payload); continue; }
      if (opcode === 10) continue;
      if (opcode === 0 && frag) { frag[1].push(payload); if (final) { const [op, parts] = frag; frag = null; deliver(op, concat(parts)); } continue; }
      if (!final) { frag = [opcode, [payload]]; continue; }
      deliver(opcode, payload);
    }
  }

  function deliver(opcode, payload) {
    if (opcode === 1) h.onmessage(dec.decode(payload));
    else if (opcode === 2) h.onmessage(payload);
  }

  let req = `GET ${path} HTTP/1.1\r\nHost: camera\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n` +
    `Sec-WebSocket-Key: ${key}\r\nSec-WebSocket-Version: 13\r\n`;
  if (protocols && protocols.length) req += `Sec-WebSocket-Protocol: ${protocols.join(', ')}\r\n`;
  s.write(enc.encode(req + '\r\n'));

  return {
    sendText(t) { send(1, enc.encode(t)); },
    sendBinary(b) { send(2, b); },
    close(code = 1000, reason = '') {
      if (closed) return;
      const r = enc.encode(reason);
      const p = new Uint8Array(2 + r.length);
      p[0] = code >> 8; p[1] = code & 255; p.set(r, 2);
      send(8, p);
      setTimeout(() => { s.end(); fin(code, reason, true); }, 1000);
    },
    buffered() { return s.pending.reduce((n, b) => n + b.length, 0); },
  };
}

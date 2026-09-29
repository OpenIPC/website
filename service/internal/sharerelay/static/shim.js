// Put into every camera page the share shows, before its own scripts: the
// camera's WebSockets (live video, logs, signalling) cannot be opened from
// here directly -- the camera is not on this network -- so the shell's
// tunnel carries them. Everything else about the page is the camera's own.
(() => {
  const P = window.parent && window.parent !== window && window.parent.__share;
  if (!P) return;
  const Native = window.WebSocket;
  class TunnelWebSocket extends EventTarget {
    constructor(url, protocols) {
      super();
      const u = new URL(url, location.href);
      if (u.host !== location.host) return new Native(url, protocols);
      this.url = u.href.replace(/^http/, 'ws');
      this.readyState = 0;
      this.protocol = '';
      this.extensions = '';
      this.binaryType = 'blob';
      this.onopen = this.onmessage = this.onerror = this.onclose = null;
      const fire = (type, init) => {
        const ev = type === 'message' ? new MessageEvent('message', init)
          : type === 'close' ? new CloseEvent('close', init) : new Event(type);
        const h = this['on' + type];
        if (typeof h === 'function') h.call(this, ev);
        this.dispatchEvent(ev);
      };
      this._ws = P.openWebSocket(u.pathname + u.search, [].concat(protocols || []), {
        onopen: (proto) => { this.readyState = 1; this.protocol = proto || ''; fire('open'); },
        onmessage: (d) => {
          if (typeof d !== 'string') d = this.binaryType === 'arraybuffer' ? d.buffer.slice(d.byteOffset, d.byteOffset + d.byteLength) : new Blob([d]);
          fire('message', { data: d, origin: location.origin });
        },
        onerror: () => fire('error'),
        onclose: (code, reason, clean) => { this.readyState = 3; fire('close', { code, reason, wasClean: clean }); },
      });
    }
    get bufferedAmount() { return this._ws ? this._ws.buffered() : 0; }
    send(d) {
      if (this.readyState !== 1) throw new DOMException('not open', 'InvalidStateError');
      if (typeof d === 'string') this._ws.sendText(d);
      else if (d instanceof Blob) d.arrayBuffer().then((b) => this._ws.sendBinary(new Uint8Array(b)));
      else if (d instanceof ArrayBuffer) this._ws.sendBinary(new Uint8Array(d));
      else this._ws.sendBinary(new Uint8Array(d.buffer, d.byteOffset, d.byteLength));
    }
    close(code, reason) {
      if (this.readyState >= 2) return;
      this.readyState = 2;
      this._ws.close(code || 1000, reason || '');
    }
  }
  for (const [k, v] of Object.entries({ CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 })) {
    TunnelWebSocket[k] = v;
    TunnelWebSocket.prototype[k] = v;
  }
  window.WebSocket = TunnelWebSocket;
})();

// The view-only guest's player: the camera's live video, the way the camera's
// own WebUI plays it, in the same order.
//
// A view-only link opens the camera's media and nothing else, so the WebUI's
// player is out of reach; this is the share page's own. It walks the WebUI's
// chain:
//
//   1. WebRTC. Signalling over /ws/webrtc, carried by the tunnel; the media on
//      its own peer connection straight to the camera, with the share's ICE
//      servers. Sub-second latency, the browser's hardware decoder, a camera
//      that responds to the guest's link -- and the tunnel's data channel
//      left to carry nothing heavier than the signalling.
//   2. MSE, over /ws/video through the tunnel. Negotiates nothing, so it plays
//      what WebRTC will not: a codec or profile this browser's WebRTC stack
//      does not offer (Firefox offers H.264 Baseline only), or a path the
//      media cannot take.
//   3. The camera's MJPEG, which every browser shows, with the reason the
//      guest is on it -- a slower picture nobody explains reads as a broken one.
//
// Each rung is tried only when the one above it has failed in this page.

const MS = window.MediaSource || window.ManagedMediaSource;
const MANAGED = !!MS && MS !== window.MediaSource;
const RTC = typeof window.RTCPeerConnection === 'function';
// Audio the browser can take in MP4, best first; the camera picks among them.
const AUDIO = ['opus', 'mp4a.40.2'];
// WebRTC: how long to wait for a picture (ICE and DTLS come first), and how
// long one may freeze before the session is taken for dead -- a stalled
// stream is invisible to signalling, the socket and ICE both stay up.
const RTC_FIRST_MS = 10000;
const RTC_STALL_MS = 8000;
// MSE: how far behind the newest frame playback may drift before it is moved
// back to the live edge, where it lands, and how much played video to keep.
const LIVE_LAG = 1.5;
const LIVE_LAND = 0.2;
const KEEP = 10;
// Safari's HEVC decoder stalls on a fragment per append; a few at a time it
// keeps up with. H.264 goes one by one, for the lower latency.
const HEVC_BATCH = 5;
const HEVC_WAIT = 200;
// Strikes before giving up on a rung.
const DECODE_MAX = 2;
const RECONNECT_MAX = 5;

function el(tag, props, ...kids) {
  const n = Object.assign(document.createElement(tag), props || {});
  for (const k of kids) if (k) n.append(k);
  return n;
}

// The fragment as MSE takes it: a producer-reference-time box in front, which
// the camera adds for its own players' latency figure, is stripped.
function stripPrft(u8) {
  if (u8.length < 32 || u8[4] !== 0x70 || u8[5] !== 0x72 || u8[6] !== 0x66 || u8[7] !== 0x74) return u8;
  const size = ((u8[0] << 24) | (u8[1] << 16) | (u8[2] << 8) | u8[3]) >>> 0;
  return size >= 32 && size <= u8.length ? u8.subarray(size) : u8;
}

function concat(parts) {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let o = 0;
  for (const p of parts) { out.set(p, o); o += p.length; }
  return out;
}

// mount(main, { openWebSocket, iceServers, camera, trace }): fills `main`
// with the player. `iceServers` may be a promise.
export function mount(main, { openWebSocket, iceServers, camera, trace }) {
  const video = el('video', { muted: true, autoplay: true, playsInline: true });
  video.setAttribute('playsinline', '');
  if (MANAGED) video.disableRemotePlayback = true;
  const img = el('img', { alt: 'Live video' });
  img.hidden = true;
  const note = el('div', { className: 'player-note' });
  note.hidden = true;
  const sound = el('button', { type: 'button', textContent: 'Sound on' });
  const shot = el('button', { type: 'button', textContent: 'Snapshot' });
  const full = el('button', { type: 'button', textContent: 'Full screen' });
  const controls = el('div', { className: 'player-controls' }, sound, shot, full);
  const box = el('div', { className: 'player' }, video, img, note, controls);
  main.replaceChildren(box);

  // Which rung is playing, and whatever the current attempt holds. `attempt`
  // numbers every start, so a handler left over from an earlier one does
  // nothing.
  let rung = '', attempt = 0, wantAudio = false, gone = false;
  let sig = null, pc = null, firstTimer = null, stallTimer = null, lastFrames = -1, rtcRetried = false;
  let ws = null, ms = null, sb = null, url = null, retry = null;
  let queue = [], timer = null, hevc = false, playing = '', skipBinary = false;
  let decodeErrs = 0, reconnects = 0;

  function say(text) { note.textContent = text; note.hidden = !text; }
  function soundState(text, disabled) { sound.textContent = text; sound.disabled = !!disabled; }

  // ---- teardown, per rung -------------------------------------------------

  function stopRtc() {
    clearTimeout(firstTimer); clearInterval(stallTimer);
    firstTimer = stallTimer = null;
    if (sig) { const s = sig; sig = null; try { s.close(); } catch (e) { /* gone */ } }
    if (pc) { const p = pc; pc = null; p.ontrack = p.onicecandidate = p.oniceconnectionstatechange = null; try { p.close(); } catch (e) { /* gone */ } }
    if (video.srcObject) video.srcObject = null;
  }

  // The decoder side alone: a real reconfigure (codec, size, sound) needs a
  // fresh MediaSource on the same socket.
  function dropSource() {
    clearTimeout(timer); timer = null;
    queue = [];
    sb = null;
    playing = '';
    if (ms && ms.readyState === 'open') { try { ms.endOfStream(); } catch (e) { /* closing anyway */ } }
    ms = null;
    if (url) { video.removeAttribute('src'); video.load(); URL.revokeObjectURL(url); url = null; }
  }

  function stopMse() {
    clearTimeout(retry); retry = null;
    if (ws) { const w = ws; ws = null; try { w.close(); } catch (e) { /* gone */ } }
    dropSource();
  }

  function stopAll() { attempt++; stopRtc(); stopMse(); }

  // ---- 3. MJPEG -----------------------------------------------------------

  function mjpeg(why) {
    trace('player', `mjpeg: ${why}`);
    stopAll();
    rung = 'mjpeg';
    video.hidden = true;
    sound.hidden = true;
    img.hidden = false;
    if (!img.src) img.src = '/mjpeg';
    say(why);
  }

  // ---- 1. WebRTC ----------------------------------------------------------

  async function webrtc() {
    stopAll();
    const my = attempt;
    rung = 'webrtc';
    lastFrames = -1;
    const ice = await Promise.resolve(iceServers).catch(() => []);
    if (my !== attempt || gone) return;
    const fail = (why) => {
      if (my !== attempt) return;
      trace('player', `webrtc: ${why}`);
      mse();
    };
    try {
      pc = new RTCPeerConnection({ iceServers: ice || [] });
      pc.addTransceiver('video', { direction: 'recvonly' });
      // Audio only when asked for: the camera encodes for nobody otherwise.
      if (wantAudio) pc.addTransceiver('audio', { direction: 'recvonly' });
    } catch (e) {
      fail('no peer connection');
      return;
    }
    const peer = pc;
    peer.ontrack = (ev) => {
      if (my !== attempt) return;
      video.srcObject = ev.streams && ev.streams[0] ? ev.streams[0] : new MediaStream([ev.track]);
      video.muted = !wantAudio;
      video.play().catch(() => {});
    };
    peer.onicecandidate = (ev) => { if (ev.candidate && sig) send('candidate', ev.candidate.candidate); };
    peer.oniceconnectionstatechange = () => {
      if (my !== attempt) return;
      if (peer.iceConnectionState === 'failed') again('the connection to the camera failed');
    };
    const send = (req, data) => { if (sig) sig.sendText(JSON.stringify({ req, data: data === undefined ? '' : data })); };
    // A session that played once and then dropped is worth one more WebRTC
    // try before the next rung; one that never played is not.
    let played = false;
    const again = (why) => {
      if (my !== attempt) return;
      if (played && !rtcRetried) { rtcRetried = true; trace('player', `webrtc: ${why}, once more`); webrtc(); return; }
      fail(why);
    };
    sig = openWebSocket('/ws/webrtc?stream=0', [], {
      async onopen() {
        try {
          const offer = await peer.createOffer();
          await peer.setLocalDescription(offer);
          if (my === attempt) send('offer', peer.localDescription.sdp);
        } catch (e) { fail('could not make an offer'); }
      },
      onmessage(d) {
        if (my !== attempt || typeof d !== 'string') return;
        let m;
        try { m = JSON.parse(d); } catch (e) { return; }
        if (!m || typeof m.reply !== 'string') return;
        if (m.reply === 'answer') {
          peer.setRemoteDescription({ type: 'answer', sdp: m.data }).then(() => {
            if (my !== attempt || !wantAudio) return;
            // Did the camera take the audio offered? The negotiated
            // direction says, before any track event does.
            const a = peer.getTransceivers().find((t) => t.receiver.track && t.receiver.track.kind === 'audio');
            const ok = a && /recv/.test(a.currentDirection || '');
            soundState(ok ? 'Sound off' : 'No sound', !ok);
            if (!ok) { wantAudio = false; video.muted = true; }
          }).catch(() => fail('the camera’s answer was refused'));
        } else if (m.reply === 'candidate') {
          peer.addIceCandidate({ candidate: m.data, sdpMid: m.mid }).catch(() => {});
        } else if (m.reply === 'busy' || m.reply === 'error') {
          fail(`${m.reply}: ${m.data || ''}`);
        } else if (m.reply === 'closed') {
          again(`closed: ${m.data || ''}`);
        }
      },
      onerror() {},
      onclose() {
        if (my !== attempt) return;
        // The signalling socket may end once the session is up; the media
        // does not ride it. Before then, it is the end of this attempt.
        if (!played) fail('the signalling closed');
      },
    });
    firstTimer = setTimeout(() => { if (!played) fail('no picture arrived'); }, RTC_FIRST_MS);
    const onPlaying = () => {
      if (my !== attempt || played) return;
      played = true;
      clearTimeout(firstTimer);
      say('');
      trace('player', 'webrtc playing');
      // The frame counter is the only witness to a frozen picture.
      stallTimer = setInterval(async () => {
        if (my !== attempt || !pc) return;
        let frames = -1;
        try {
          (await pc.getStats()).forEach((r) => { if (r.type === 'inbound-rtp' && r.kind === 'video') frames = r.framesDecoded || 0; });
        } catch (e) { return; }
        if (frames === lastFrames && frames >= 0) again('the picture froze');
        lastFrames = frames;
      }, RTC_STALL_MS);
    };
    video.addEventListener('playing', onPlaying, { once: true });
  }

  // ---- 2. MSE -------------------------------------------------------------

  function mse() {
    stopAll();
    rung = 'mse';
    if (!MS) { mjpeg('This browser cannot play the camera’s video directly, so a slower picture is shown instead.'); return; }
    reconnects = 0;
    mseOpen();
  }

  function pump() {
    if (!sb || sb.updating || !queue.length) return;
    if (MANAGED && ms && ms.streaming === false) return;
    if (hevc && queue.length < HEVC_BATCH && !timer) {
      timer = setTimeout(() => { timer = null; pump(); }, HEVC_WAIT);
      return;
    }
    clearTimeout(timer);
    timer = null;
    const bytes = hevc ? concat(queue.splice(0)) : queue.shift();
    try {
      sb.appendBuffer(bytes);
    } catch (e) {
      // A full buffer: drop what has been played and try again.
      if (e.name === 'QuotaExceededError' && video.currentTime > KEEP) {
        queue.unshift(bytes);
        sb.remove(0, video.currentTime - 2);
        return;
      }
      mjpeg('This browser stopped playing the camera’s video, so a slower picture is shown instead.');
    }
  }

  function edge() {
    if (!sb || !video.buffered.length) return;
    const end = video.buffered.end(video.buffered.length - 1);
    if (end - video.currentTime > LIVE_LAG) video.currentTime = end - LIVE_LAND;
    if (!sb.updating && video.currentTime - video.buffered.start(0) > KEEP * 2) {
      sb.remove(0, video.currentTime - KEEP);
    }
    if (video.paused) video.play().catch(() => {});
  }

  function onInit(info) {
    const mime = info.mime || `video/mp4; codecs="${info.codecString}"`;
    if (wantAudio) {
      soundState(info.audioCodec ? 'Sound off' : 'No sound', !info.audioCodec);
      video.muted = !info.audioCodec;
    }
    if (!MS.isTypeSupported(mime)) {
      mjpeg(`This browser cannot play the camera’s ${String(info.codec || 'video').toUpperCase()} video, so a slower picture is shown instead.`);
      return;
    }
    const key = `${mime}|${info.width}x${info.height}`;
    // The camera re-sends an unchanged init with every keyframe's parameter
    // sets on some encoders. Rebuilding for it blanks the picture each time;
    // the fragments carry on the same timeline, so keep the running decoder
    // and drop the init segment that follows.
    if (key === playing && sb) { skipBinary = true; return; }
    dropSource();
    playing = key;
    hevc = /hvc1|hev1/.test(mime);
    ms = new MS();
    url = URL.createObjectURL(ms);
    video.src = url;
    ms.addEventListener('sourceopen', () => {
      try {
        sb = ms.addSourceBuffer(mime);
      } catch (e) {
        mjpeg('This browser refused the camera’s video, so a slower picture is shown instead.');
        return;
      }
      sb.mode = 'segments';
      sb.addEventListener('updateend', () => { edge(); pump(); });
      if (MANAGED) ms.addEventListener('startstreaming', pump);
      pump();
    }, { once: true });
    trace('player', `mse ${info.codec} ${info.width}x${info.height}`);
  }

  function mseOpen() {
    if (gone) return;
    const my = attempt;
    let q = '/ws/video?stream=0';
    if (wantAudio) {
      const a = AUDIO.filter((c) => MS.isTypeSupported(`audio/mp4; codecs="${c}"`));
      if (a.length) q += '&audio=' + a.join(',');
    }
    let started = false;
    const sock = openWebSocket(q, [], {
      onopen() { sock.sendText(JSON.stringify({ request: 'idr' })); },
      onmessage(d) {
        if (my !== attempt || ws !== sock) return;
        if (typeof d === 'string') {
          let info;
          try { info = JSON.parse(d); } catch (e) { return; }
          if (info && info.type === 'init') { started = true; reconnects = 0; onInit(info); }
          return;
        }
        if (skipBinary) { skipBinary = false; return; }
        if (!playing) return;
        queue.push(stripPrft(d));
        if (queue.length > 240) queue.splice(0, queue.length - 240);
        pump();
      },
      onerror() {},
      onclose() {
        if (my !== attempt || ws !== sock || gone) return;
        ws = null;
        stopMse();
        if (++reconnects > RECONNECT_MAX) {
          mjpeg('The camera’s video would not stay connected, so a slower picture is shown instead.');
          return;
        }
        say(started ? 'Reconnecting…' : '');
        retry = setTimeout(() => { if (my === attempt) mseOpen(); }, Math.min(1000 * 2 ** (reconnects - 1), 8000));
      },
    });
    ws = sock;
  }

  // ---- shared -------------------------------------------------------------

  video.addEventListener('playing', () => { if (rung !== 'mjpeg') say(''); });
  video.addEventListener('error', () => {
    if (rung !== 'mse' || !sb) return;
    trace('player', `decode error ${video.error && video.error.code}`);
    if (++decodeErrs >= DECODE_MAX) {
      mjpeg('This browser could not decode the camera’s video, so a slower picture is shown instead.');
      return;
    }
    // Once more from a keyframe, on a fresh decoder.
    mse();
  });

  sound.addEventListener('click', () => {
    if (wantAudio) {
      video.muted = !video.muted;
      soundState(video.muted ? 'Sound on' : 'Sound off');
      return;
    }
    // The camera encodes sound only for a viewer who asked for it: ask, on
    // whichever rung is playing.
    wantAudio = true;
    soundState('Sound…', true);
    if (rung === 'webrtc') webrtc(); else mse();
  });

  shot.addEventListener('click', async () => {
    shot.disabled = true;
    try {
      const r = await fetch('/image.jpg');
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const blob = await r.blob();
      const stamp = new Date().toISOString().slice(0, 19).replace(/[T:]/g, '-');
      const name = `${(camera || 'camera').replace(/[^\w.-]+/g, '_')}-${stamp}.jpg`;
      const a = el('a', { href: URL.createObjectURL(blob), download: name });
      document.body.append(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(a.href), 10000);
    } catch (e) {
      say('The snapshot could not be taken. Try again.');
    } finally {
      shot.disabled = false;
    }
  });

  full.addEventListener('click', () => {
    if (document.fullscreenElement) { document.exitFullscreen().catch(() => {}); return; }
    if (box.requestFullscreen) box.requestFullscreen().catch(() => {});
    else if (video.webkitEnterFullscreen && !video.hidden) video.webkitEnterFullscreen();
  });
  if (!document.fullscreenEnabled && !video.webkitEnterFullscreen) full.hidden = true;

  if (RTC) webrtc(); else mse();

  return {
    get rung() { return rung; },
    close() { gone = true; stopAll(); img.removeAttribute('src'); },
  };
}

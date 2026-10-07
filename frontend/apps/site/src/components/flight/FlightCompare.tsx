/**
 * A real flight, as the drone recorded it and as the pilot saw it.
 *
 * Two recordings of one flight, cut onto one timeline by tools/flight-ab and
 * streamed as two DASH presentations from /media/. The drone's side is on top,
 * clipped at the split; the ground station's is underneath. Same arrangement
 * and the same way of keeping them on one frame as the R&D Player's compare
 * (../../lib/flight-sync.ts), without the player around it.
 *
 * Nothing is fetched until the reader presses play: before that this is a
 * poster, so the page weighs what it did. Shaka arrives then, as its own
 * chunk.
 *
 * Each side adapts on its own, and each says what it is showing, because a
 * comparison where one side has quietly dropped to 540p is not one. "Original
 * quality" pins both to the recordings themselves.
 */
import { useEffect, useRef, useState } from 'preact/hooks';
import type shakaNs from 'shaka-player/dist/shaka-player.dash.js';
import { clock, originalOf, rungKind, SEEK_DRIFT, syncAction, type RungKind } from '../../lib/flight-sync';

type Shaka = typeof shakaNs;
type Player = InstanceType<Shaka['Player']>;
type Side = 'onboard' | 'gs';

export interface FlightLabels {
  play: string;
  pause: string;
  loading: string;
  error: string;
  failed: string;
  onboard: string;
  gs: string;
  original: string;
  reduced: string;
  copy: string;
  fullscreen: string;
  frameBack: string;
  frameForward: string;
  handle: string;
  seek: string;
  openRnd: string;
}

interface Props {
  /** The flight's directory under /media/, with a trailing slash. */
  base: string;
  poster: string;
  /** The common window, on the drone's timeline, from stats.json. */
  start: number;
  end: number;
  rndPlayer: string;
  labels: FlightLabels;
}

type Phase = 'idle' | 'loading' | 'ready' | 'failed' | 'unsupported';
interface Badge { height: number | null; kind: RungKind }

const FRAME = 1 / 60;

// Drawn rather than typed: ⏮ ⏭ ⇆ are missing from enough system fonts to
// arrive as empty boxes.
const Glyph = ({ d }: { d: string }) => (
  <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="currentColor"><path d={d} /></svg>
);
const STEP_BACK = 'M3 3h2v10H3zM13 3v10L6 8z';
const STEP_FORWARD = 'M11 3h2v10h-2zM3 3v10l7-5z';
const BOTH_WAYS = 'M1 8l4-4v3h6V4l4 4-4 4V9H5v3z';

export default function FlightCompare({ base, poster, start, end, rndPlayer, labels }: Props) {
  // Never exactly on the ground station's first frame: a playhead a rounding
  // error before its buffered range has no picture, and Shaka steps over that
  // gap only while the video plays -- which a side held for the other never
  // does, so both waited for good. Five milliseconds in is the same frame.
  const begin = start + 0.005;
  const box = useRef<HTMLDivElement>(null);
  const lead = useRef<HTMLVideoElement>(null);
  const follow = useRef<HTMLVideoElement>(null);
  const players = useRef<Partial<Record<Side, Player>>>({});
  const held = useRef(false);

  const [phase, setPhase] = useState<Phase>('idle');
  const [playing, setPlaying] = useState(false);
  const [time, setTime] = useState(begin);
  const [split, setSplit] = useState(50);
  const [pinned, setPinned] = useState(false);
  const [badges, setBadges] = useState<Partial<Record<Side, Badge>>>({});

  const describe = (side: Side) => {
    const p = players.current[side];
    if (!p) return;
    const tracks = p.getVariantTracks();
    const active = tracks.find((t) => t.active);
    if (!active) return;
    setBadges((b) => ({ ...b, [side]: { height: active.height, kind: rungKind(side, active, tracks) } }));
  };

  // Both players go together: one left attached beside a failed other would
  // keep its buffers for nothing.
  async function teardown() {
    const ps = Object.values(players.current);
    players.current = {};
    held.current = false;
    setBadges({});
    setPlaying(false);
    await Promise.all(ps.map((p) => p?.destroy().catch(() => {})));
  }

  // A browser without MSE is told so; anything else -- the media tree missing,
  // a network failure, a stream that breaks mid-flight -- is a failure the
  // reader can retry, and is logged rather than blamed on the browser.
  async function fail(err: unknown) {
    console.error('flight: the streams failed', err);
    await teardown();
    setPhase('failed');
  }

  async function load() {
    setPhase('loading');
    try {
      const { default: shaka } = await import('shaka-player/dist/shaka-player.dash.js');
      shaka.polyfill.installAll();
      if (!shaka.Player.isBrowserSupported()) { setPhase('unsupported'); return; }
      const sides: [Side, HTMLVideoElement][] = [['onboard', lead.current!], ['gs', follow.current!]];
      await Promise.all(sides.map(async ([side, video]) => {
        const p = new shaka.Player();
        await p.attach(video);
        p.configure({
          // HEVC first: the ground station's recording is HEVC, and its H.264
          // rungs are for browsers that cannot decode it.
          preferredVideoCodecs: ['hvc1', 'hev1', 'avc1'],
          // No playRangeStart: Shaka makes it the MSE append window, and the
          // browser then drops every frame up to the next keyframe -- 4 s of
          // the drone's side. The window is kept by the controls below instead.
          streaming: { bufferingGoal: 20, rebufferingGoal: 1 },
          abr: { defaultBandwidthEstimate: 8_000_000 },
        });
        p.addEventListener('error', (e) => {
          const err = (e as unknown as { detail?: { severity?: number } }).detail;
          if (err?.severity === shaka.util.Error.Severity.CRITICAL) void fail(err);
        });
        p.addEventListener('adaptation', () => describe(side));
        p.addEventListener('variantchanged', () => describe(side));
        players.current[side] = p;
        await p.load(`${base}${side}.mpd`, begin);
        describe(side);
      }));
    } catch (err) {
      await fail(err);
      return;
    }
    setPhase('ready');
    // Outside the try: a play() cut short by the buffering hold below rejects
    // with AbortError, which is not the stream failing to load.
    void lead.current!.play().catch(() => {});
  }

  // Everything the follower does, it does because the leader did it.
  useEffect(() => {
    const a = lead.current, b = follow.current;
    if (!a || !b || phase !== 'ready') return;
    const onPlay = () => { setPlaying(true); void b.play().catch(() => {}); };
    const onPause = () => {
      // A pause event is dispatched after the fact: one from a hold that has
      // already been released would otherwise stop the follower for good.
      if (held.current || !a.paused) return;
      setPlaying(false);
      b.pause();
      b.currentTime = a.currentTime;
    };
    const onSeek = () => {
      // Before the window the ground station has no picture yet.
      if (a.currentTime < begin) { a.currentTime = begin; return; }
      setTime(a.currentTime);
      // Not while the follower is still starting: a seek that lands before
      // Shaka's first append left it seeking with nothing buffered for good
      // (about one start in six). The hold below lines it up once it has a
      // picture.
      if (b.readyState >= 2) b.currentTime = a.currentTime;
    };
    const onEnded = () => { setPlaying(false); b.pause(); };
    a.addEventListener('play', onPlay);
    a.addEventListener('pause', onPause);
    a.addEventListener('seeking', onSeek);
    a.addEventListener('ended', onEnded);

    let raf = 0;
    let shown = -1;
    const tick = () => {
      raf = requestAnimationFrame(tick);
      // Read off the element rather than kept from events: the first play()
      // runs before this effect has attached its listeners.
      setPlaying(!a.paused || held.current);
      // The clock under the picture, a few times a second rather than every
      // frame: it reads in whole seconds.
      if (Math.abs(a.currentTime - shown) >= 0.25 || a.paused) {
        shown = a.currentTime;
        setTime(shown);
      }
      // If either side is starved, both wait: a side that ran on alone would
      // be compared with a picture from seconds before. Seeking and Shaka's
      // own buffering count as starved -- a leader that kept playing while
      // the follower sought would be further ahead when the seek landed, and
      // the follower would chase it for ever.
      const pa = players.current.onboard, pb = players.current.gs;
      const aStarved = a.readyState < 3 || a.seeking || !!pa?.isBuffering();
      const bStarved = b.readyState < 3 || b.seeking || !!pb?.isBuffering();
      const starved = aStarved || bStarved;
      if (!a.paused && starved) {
        held.current = true;
        a.pause();
      }
      if (held.current && starved) {
        // A starved follower is left playing: it cannot move without data,
        // and Shaka gets a stalled stream going again only while it plays --
        // paused while still starting, it was seen to wait for ever. Only a
        // follower that has data, and would run ahead, is stopped.
        if (bStarved && b.paused) void b.play().catch(() => {});
        if (!bStarved && !b.paused) b.pause();
        return;
      }
      if (held.current && !starved) {
        // Line the follower up with the stopped leader if it is really
        // elsewhere, then go. Only a real gap: a follower left running while
        // it waited is a few milliseconds on by the time it is seen ready, and
        // re-seeking that -- a seek decodes from the keyframe before it -- had
        // the two take turns being ready for good. The nudge closes the rest.
        if (Math.abs(b.currentTime - a.currentTime) > SEEK_DRIFT) { b.currentTime = a.currentTime; return; }
        held.current = false;
        b.playbackRate = 1;
        void a.play().catch(() => {});
        void b.play().catch(() => {});
        return;
      }
      if (a.paused || b.seeking) return;
      if (b.paused) void b.play().catch(() => {});
      const act = syncAction(a.currentTime, b.currentTime);
      if (act.kind === 'seek') b.currentTime = act.to;
      else if (b.playbackRate !== act.rate) b.playbackRate = act.rate;
    };
    raf = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(raf);
      a.removeEventListener('play', onPlay);
      a.removeEventListener('pause', onPause);
      a.removeEventListener('seeking', onSeek);
      a.removeEventListener('ended', onEnded);
    };
  }, [phase]);

  useEffect(() => () => {
    for (const p of Object.values(players.current)) void p?.destroy();
  }, []);

  async function pin(on: boolean) {
    setPinned(on);
    for (const side of ['onboard', 'gs'] as Side[]) {
      const p = players.current[side];
      if (!p) continue;
      p.configure({ abr: { enabled: !on } });
      if (on) {
        const top = originalOf(side, p.getVariantTracks());
        if (top) p.selectVariantTrack(top, true);
      }
      describe(side);
    }
  }

  const toggle = () => {
    const a = lead.current;
    if (phase === 'idle' || phase === 'failed') { void load(); return; }
    if (!a || phase !== 'ready') return;
    // Held for a starved side counts as playing: Pause has to stop both,
    // including a follower left running while it waits for data.
    if (held.current) { held.current = false; follow.current?.pause(); return; }
    if (a.paused) void a.play(); else a.pause();
  };

  const step = (by: number) => {
    const a = lead.current;
    if (!a || phase !== 'ready') return;
    // A step is a decision to stop here: a hold must not resume over it.
    held.current = false;
    a.pause();
    follow.current?.pause();
    a.currentTime = Math.min(end, Math.max(begin, a.currentTime + by));
  };

  // The split follows the pointer while it is down anywhere on the picture,
  // which is what makes it usable on a phone, where the handle is a hairline.
  const dragging = useRef(false);
  const place = (clientX: number) => {
    const r = box.current!.getBoundingClientRect();
    setSplit(Math.min(100, Math.max(0, ((clientX - r.left) * 100) / r.width)));
  };

  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'ArrowLeft') { setSplit((s) => Math.max(0, s - 2)); e.preventDefault(); }
    if (e.key === 'ArrowRight') { setSplit((s) => Math.min(100, s + 2)); e.preventDefault(); }
  };

  const clip = `inset(0 ${100 - split}% 0 0)`;

  const badge = (side: Side) => {
    const b = badges[side];
    if (!b) return null;
    const what = b.kind === 'original' ? labels.original : b.kind === 'copy' ? labels.copy : labels.reduced;
    return (
      // On a phone only the warning: two full badges do not fit beside the labels.
      <span class={`ms-2 rounded px-1.5 py-0.5 font-mono text-[11px] ${b.kind === 'reduced' ? 'inline bg-amber-400 text-black' : 'hidden bg-black/60 text-white sm:inline'}`}>
        {b.height ? `${b.height}p · ` : ''}{what}
      </span>
    );
  };

  const showOnboardLabel = split > 0;
  const showGsLabel = split < 100;

  return (
    <figure class="m-0">
      <div
        ref={box}
        class="relative aspect-video w-full touch-none overflow-hidden rounded-lg bg-black select-none"
        onPointerDown={(e) => {
          if (phase !== 'ready') return;
          dragging.current = true;
          (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
          place(e.clientX);
        }}
        onPointerMove={(e) => { if (dragging.current) place(e.clientX); }}
        onPointerUp={() => { dragging.current = false; }}
        onPointerCancel={() => { dragging.current = false; }}
      >
        <video ref={follow} class="absolute inset-0 size-full object-contain" muted playsInline preload="none" />
        <video
          ref={lead}
          class="absolute inset-0 size-full object-contain"
          style={{ clipPath: clip }}
          muted
          playsInline
          preload="none"
        />

        {phase !== 'ready' && (
          <button
            type="button"
            class="absolute inset-0 flex size-full cursor-pointer items-center justify-center border-0 bg-cover bg-center p-0"
            style={{ backgroundImage: `url(${poster})` }}
            onClick={toggle}
            disabled={phase === 'loading' || phase === 'unsupported'}
            aria-label={labels.play}
          >
            <span class="rounded-full bg-black/70 px-5 py-3 text-base font-semibold text-white">
              {phase === 'loading' ? labels.loading : phase === 'unsupported' ? labels.error : phase === 'failed' ? labels.failed : labels.play}
            </span>
          </button>
        )}

        <div class="pointer-events-none absolute inset-x-0 top-0 flex justify-between p-2 text-xs font-semibold text-white sm:text-sm">
          <span class={`rounded bg-black/60 px-2 py-1 ${showOnboardLabel ? '' : 'invisible'}`}>{labels.onboard}{badge('onboard')}</span>
          <span class={`rounded bg-black/60 px-2 py-1 ${showGsLabel ? '' : 'invisible'}`}>{labels.gs}{badge('gs')}</span>
        </div>

        {/* Only once there is something to divide: over the poster it would
            sit on the play button and take its click. */}
        {phase === 'ready' && (
          <div
            class="absolute inset-y-0 w-0.5 -translate-x-1/2 bg-white/90 shadow"
            style={{ left: `${split}%` }}
          >
            <div
              role="slider"
              tabIndex={0}
              aria-label={labels.handle}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={Math.round(split)}
              onKeyDown={onKey}
              class="absolute top-1/2 left-1/2 flex size-9 -translate-1/2 cursor-ew-resize items-center justify-center rounded-full bg-white text-sm text-black shadow focus-visible:outline-2 focus-visible:outline-brand-blue"
            >
              <Glyph d={BOTH_WAYS} />
            </div>
          </div>
        )}
      </div>

      <div class="mt-3 flex flex-wrap items-center gap-x-3 gap-y-2 text-sm">
        <button type="button" class="site-btn site-btn-primary min-w-24" onClick={toggle} disabled={phase === 'loading' || phase === 'unsupported'}>
          {playing ? labels.pause : labels.play}
        </button>
        <button type="button" class="site-btn site-btn-outline-primary px-3" onClick={() => step(-FRAME)} disabled={phase !== 'ready'} aria-label={labels.frameBack} title={labels.frameBack}><Glyph d={STEP_BACK} /></button>
        <button type="button" class="site-btn site-btn-outline-primary px-3" onClick={() => step(FRAME)} disabled={phase !== 'ready'} aria-label={labels.frameForward} title={labels.frameForward}><Glyph d={STEP_FORWARD} /></button>
        <input
          type="range"
          class="min-w-40 flex-1 accent-brand-blue"
          min={begin}
          max={end}
          step={FRAME}
          value={time}
          disabled={phase !== 'ready'}
          aria-label={labels.seek}
          onInput={(e) => { if (lead.current) lead.current.currentTime = Number((e.target as HTMLInputElement).value); }}
        />
        <span class="font-mono text-xs tabular-nums text-body-secondary">{clock(time - start)} / {clock(end - start)}</span>
      </div>

      <div class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
        <label class="inline-flex items-center gap-2">
          <input type="checkbox" checked={pinned} disabled={phase !== 'ready'} onChange={(e) => void pin((e.target as HTMLInputElement).checked)} />
          {labels.original}
        </label>
        <button type="button" class="border-0 bg-transparent p-0 text-brand-blue underline" onClick={() => void box.current?.requestFullscreen?.()}>
          {labels.fullscreen}
        </button>
        <a href={rndPlayer} target="_blank" rel="noopener">{labels.openRnd}</a>
      </div>
    </figure>
  );
}

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
import { clock, originalOf, rungKind, syncAction, type RungKind } from '../../lib/flight-sync';

type Shaka = typeof shakaNs;
type Player = InstanceType<Shaka['Player']>;
type Side = 'onboard' | 'gs';

export interface FlightLabels {
  play: string;
  pause: string;
  loading: string;
  error: string;
  onboard: string;
  gs: string;
  split: string;
  flicker: string;
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

type Phase = 'idle' | 'loading' | 'ready' | 'error';
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
const FLICKER_MS = 500;

export default function FlightCompare({ base, poster, start, end, rndPlayer, labels }: Props) {
  const box = useRef<HTMLDivElement>(null);
  const lead = useRef<HTMLVideoElement>(null);
  const follow = useRef<HTMLVideoElement>(null);
  const players = useRef<Partial<Record<Side, Player>>>({});
  const held = useRef(false);

  const [phase, setPhase] = useState<Phase>('idle');
  const [playing, setPlaying] = useState(false);
  const [time, setTime] = useState(start);
  const [split, setSplit] = useState(50);
  const [mode, setMode] = useState<'split' | 'flicker'>('split');
  const [flickerOn, setFlickerOn] = useState(true);
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

  async function load() {
    setPhase('loading');
    try {
      const { default: shaka } = await import('shaka-player/dist/shaka-player.dash.js');
      shaka.polyfill.installAll();
      if (!shaka.Player.isBrowserSupported()) throw new Error('no MSE');
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
        p.addEventListener('adaptation', () => describe(side));
        p.addEventListener('variantchanged', () => describe(side));
        players.current[side] = p;
        await p.load(`${base}${side}.mpd`, start);
        describe(side);
      }));
    } catch {
      setPhase('error');
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
      if (a.currentTime < start) { a.currentTime = start; return; }
      b.currentTime = a.currentTime;
      setTime(a.currentTime);
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
      // be compared with a picture from seconds before.
      const starved = a.readyState < 3 || b.readyState < 3;
      if (!a.paused && starved && !a.seeking && !b.seeking) {
        held.current = true;
        a.pause();
        b.pause();
        return;
      }
      if (held.current && !starved) {
        held.current = false;
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

  useEffect(() => {
    if (mode !== 'flicker') { setFlickerOn(true); return; }
    const id = setInterval(() => setFlickerOn((v) => !v), FLICKER_MS);
    return () => clearInterval(id);
  }, [mode]);

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
    if (phase === 'idle') { void load(); return; }
    if (!a || phase !== 'ready') return;
    held.current = false;
    if (a.paused) void a.play(); else a.pause();
  };

  const step = (by: number) => {
    const a = lead.current;
    if (!a || phase !== 'ready') return;
    a.pause();
    a.currentTime = Math.min(end, Math.max(start, a.currentTime + by));
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

  const clip = mode === 'flicker'
    ? (flickerOn ? 'none' : 'inset(0 100% 0 0)')
    : `inset(0 ${100 - split}% 0 0)`;

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

  const showOnboardLabel = mode === 'split' ? split > 0 : flickerOn;
  const showGsLabel = mode === 'split' ? split < 100 : !flickerOn;

  return (
    <figure class="m-0">
      <div
        ref={box}
        class="relative aspect-video w-full touch-none overflow-hidden rounded-lg bg-black select-none"
        onPointerDown={(e) => {
          if (phase !== 'ready' || mode !== 'split') return;
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
            disabled={phase === 'loading'}
            aria-label={labels.play}
          >
            <span class="rounded-full bg-black/70 px-5 py-3 text-base font-semibold text-white">
              {phase === 'loading' ? labels.loading : phase === 'error' ? labels.error : labels.play}
            </span>
          </button>
        )}

        <div class="pointer-events-none absolute inset-x-0 top-0 flex justify-between p-2 text-xs font-semibold text-white sm:text-sm">
          <span class={`rounded bg-black/60 px-2 py-1 ${showOnboardLabel ? '' : 'invisible'}`}>{labels.onboard}{badge('onboard')}</span>
          <span class={`rounded bg-black/60 px-2 py-1 ${showGsLabel ? '' : 'invisible'}`}>{labels.gs}{badge('gs')}</span>
        </div>

        {/* Only once there is something to divide: over the poster it would
            sit on the play button and take its click. */}
        {phase === 'ready' && mode === 'split' && (
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
        <button type="button" class="site-btn site-btn-primary min-w-24" onClick={toggle} disabled={phase === 'loading'}>
          {playing ? labels.pause : labels.play}
        </button>
        <button type="button" class="site-btn site-btn-outline-primary px-3" onClick={() => step(-FRAME)} disabled={phase !== 'ready'} aria-label={labels.frameBack} title={labels.frameBack}><Glyph d={STEP_BACK} /></button>
        <button type="button" class="site-btn site-btn-outline-primary px-3" onClick={() => step(FRAME)} disabled={phase !== 'ready'} aria-label={labels.frameForward} title={labels.frameForward}><Glyph d={STEP_FORWARD} /></button>
        <input
          type="range"
          class="min-w-40 flex-1 accent-brand-blue"
          min={start}
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
        <span class="inline-flex overflow-hidden rounded border border-hairline" role="group">
          {(['split', 'flicker'] as const).map((m) => (
            <button
              type="button"
              class={`border-0 px-3 py-1 ${mode === m ? 'bg-brand-blue text-white' : 'bg-white'}`}
              aria-pressed={mode === m}
              onClick={() => setMode(m)}
            >
              {m === 'split' ? labels.split : labels.flicker}
            </button>
          ))}
        </span>
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

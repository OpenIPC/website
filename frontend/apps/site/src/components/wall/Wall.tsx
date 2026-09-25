/**
 * The Open Wall, off Rails (#165).
 *
 * One island for five addresses -- the gallery, a page of it, a camera
 * permalink, a snapshot, its archive and its slideshow -- because nginx serves
 * them all from one shell file per locale and the page learns what it is from
 * `location.pathname`. See ../../lib/wall-route.ts, which is where that
 * decision is made and tested.
 *
 * Everything here is the island's, breadcrumb and heading included, for the
 * reason the wizard's is: the page has nothing to render until it knows which
 * address it is on, and a server-rendered header over an empty body would flash
 * the wrong thing.
 *
 * No address returns a camera image (#267). Frames arrive over WallChannel,
 * masked, inside the channel's per-address budget, and are painted onto the
 * canvases below.
 */
import { useEffect, useRef, useState } from 'preact/hooks';
import {
  FRAME_DEADLINE, requestFramesOrFallBack, type FrameRequest,
} from '../../lib/wall-frames';
import { dataUrl, localised, wallAddress, type WallView } from '../../lib/wall-route';
import { useWallTranslations } from '../../lib/wall-i18n';
import { type Locale } from '../../lib/i18n';
import type { FrameVariant } from '../../lib/wall-sizes';
import Frame from './Frame';

interface Card {
  id: string;
  soc: string | null;
  sensor: string | null;
  firmware: string | null;
  streamer: string | null;
  uptime: string | null;
  soc_temperature: string | null;
  dimensions: string;
  bytes: number;
  at: number;
  caption?: string | null;
  camera?: string;
}

interface Icon { id: string; at: number }

interface Payload {
  grant: string | null;
  variant: FrameVariant;
  strip_variant?: FrameVariant;
  page?: number;
  pages?: number;
  tiles?: Card[];
  snapshot?: Card;
  strip?: Icon[];
  strip_more?: boolean;
  frames?: Icon[];
}

interface Props {
  locale: Locale;
  /** The "no signal" test card, from Astro's asset pipeline. */
  placeholder: string;
}

export default function Wall({ locale, placeholder }: Props) {
  const t = useWallTranslations(locale);
  const [address, setAddress] = useState<ReturnType<typeof wallAddress> | null>(null);
  const [data, setData] = useState<Payload | null>(null);
  const [failed, setFailed] = useState(false);
  const [painted, setPainted] = useState<ReadonlySet<string>>(new Set());
  const [resolved, setResolved] = useState(false);
  const canvases = useRef(new Map<string, HTMLCanvasElement>());

  const register = (key: string, el: HTMLCanvasElement | null) => {
    if (el) canvases.current.set(key, el); else canvases.current.delete(key);
  };

  useEffect(() => {
    const here = wallAddress(window.location.pathname);
    setAddress(here);
    if (!here.route) { setFailed(true); return undefined; }

    let live = true;
    let stop: (() => void) | undefined;
    let deadline: ReturnType<typeof setTimeout> | undefined;

    (async () => {
      let payload: Payload;
      try {
        const response = await fetch(dataUrl(here.route!), { headers: { accept: 'application/json' } });
        if (!response.ok) throw new Error(String(response.status));
        payload = (await response.json()) as Payload;
      } catch {
        if (live) { setFailed(true); setResolved(true); }
        return;
      }
      if (!live) return;

      setData(payload);
      if (!payload.grant) { setResolved(true); return; }

      // After the render that creates the canvases, not before.
      queueMicrotask(() => {
        if (!live) return;
        stop = requestFramesOrFallBack({
          grant: payload.grant!,
          requests: framesWanted(payload),
          onFrame: async (id, variant, bytes) => {
            const key = `${id}:${variant}`;
            if (await paint(canvases.current.get(key), bytes) && live) {
              setPainted((seen) => new Set(seen).add(key));
            }
          },
          onUnavailable: () => { if (live) setResolved(true); },
        });
        deadline = setTimeout(() => { if (live) setResolved(true); }, FRAME_DEADLINE);
      });
    })();

    return () => { live = false; clearTimeout(deadline); stop?.(); };
  }, []);

  if (!address) return <div class="min-h-[50vh]" />;

  const p = (path: string) => localised(address.locale, path);
  const view = address.route;

  return (
    <div class="site-container mt-4 mb-8">
      <Breadcrumb t={t} p={p} view={view} />

      <h2 class="mb-3 text-2xl font-bold">{t('title.openwall')}</h2>

      {failed && !data && (
        <p class="site-alert site-alert-warning">{t('snapshots.index.frames_unavailable')}</p>
      )}

      {view?.view === 'gallery' && data && (
        <Gallery data={data} t={t} p={p} register={register} placeholder={placeholder}
                 painted={painted} resolved={resolved} />
      )}
      {(view?.view === 'snapshot' || view?.view === 'camera') && data?.snapshot && (
        <Snapshot data={data} t={t} p={p} register={register} />
      )}
      {view?.view === 'archive' && data && (
        <Icons frames={data.frames ?? []} variant={data.variant} t={t} p={p} register={register} />
      )}
      {view?.view === 'oneday' && data && (
        <Slideshow data={data} t={t} p={p} register={register} />
      )}

      {resolved && data?.grant && painted.size === 0 && (
        <p class="site-alert site-alert-warning mt-4">{t('snapshots.index.frames_unavailable')}</p>
      )}
    </div>
  );
}

/** What each view asks the channel for, and at which size. */
function framesWanted(data: Payload): FrameRequest[] {
  if (data.tiles) return [{ variant: data.variant, ids: data.tiles.map((c) => c.id) }];
  if (data.frames) return [{ variant: data.variant, ids: data.frames.map((f) => f.id) }];
  if (!data.snapshot) return [];

  // Two variants on one subscription: the frame being read at full resolution,
  // and its camera's strip as icons. The channel takes one variant per
  // request and its grants accumulate, which is what makes that safe.
  return [
    { variant: data.variant, ids: [data.snapshot.id] },
    { variant: data.strip_variant ?? 'icon2', ids: (data.strip ?? []).map((i) => i.id) },
  ];
}

type Translate = (key: string, options?: Record<string, unknown>) => string;
type Path = (path: string) => string;

function Breadcrumb({ t, p, view }: { t: Translate; p: Path; view: WallView | null }) {
  const id = view && 'id' in view ? view.id : null;

  return (
    <nav aria-label="breadcrumb" class="mb-3 text-sm text-muted">
      <ol class="flex flex-wrap gap-2">
        <li><a href={p('/')}>{t('nav.home')}</a></li>
        <li aria-hidden="true">/</li>
        <li><a href={p('/open-wall')}>{t('nav.snapshots')}</a></li>
        {id && (
          <>
            <li aria-hidden="true">/</li>
            <li aria-current="page">{t('nav.snapshot', { number: id })}</li>
          </>
        )}
      </ol>
    </nav>
  );
}

function Gallery({ data, t, p, register, placeholder, painted, resolved }: {
  data: Payload; t: Translate; p: Path; placeholder: string;
  painted: ReadonlySet<string>; resolved: boolean;
  register: (key: string, el: HTMLCanvasElement | null) => void;
}) {
  const tiles = data.tiles ?? [];
  // The Rails page pads to nine with "no signal" cards, which is what an empty
  // wall looks like there too.
  const blanks = Math.max(0, 9 - tiles.length);

  return (
    <>
      <p class="mb-2">{t('snapshots.index.subtitle')}</p>
      <p class="mb-4 max-w-[70ch] text-sm text-muted">{t('snapshots.index.intro_html')}</p>

      <div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {tiles.map((tile) => (
          <a key={tile.id} href={p(`/snapshots/${tile.id}`)}
             class="block overflow-hidden rounded-lg border border-hairline no-underline">
            <div class="aspect-video bg-ink-2">
              <Frame id={tile.id} variant={data.variant} alt={t('snapshots.snapshot.snapshot_alt')}
                     class="size-full object-cover" register={register} />
            </div>
            <CardBody card={tile} t={t} />
          </a>
        ))}

        {Array.from({ length: blanks }, (_, i) => (
          <div key={`blank-${i}`} class="overflow-hidden rounded-lg border border-hairline">
            <img class="aspect-video w-full object-cover opacity-50" src={placeholder}
                 alt={t('snapshots.index.snapshot_alt')} loading="lazy" />
            <div class="p-3 text-sm">
              <p class="mb-1 font-bold">{t('snapshots.index.no_signal')}</p>
              <p class="mb-2 text-muted">OpenIPC</p>
              <p>{t('snapshots.index.stay_tuned')}</p>
            </div>
          </div>
        ))}
      </div>

      <Pagination page={data.page ?? 1} pages={data.pages ?? 1} p={p} />
      {resolved && tiles.length > 0 && painted.size === 0 && null}
    </>
  );
}

function CardBody({ card, t }: { card: Card; t: Translate }) {
  return (
    <div class="p-3 text-sm">
      <p class="float-right text-xs text-muted"><At at={card.at} /></p>
      <p class="mb-1 font-bold">{[card.soc, card.sensor].filter(Boolean).join(' + ').toUpperCase()}</p>
      <p class="mb-2 text-xs text-accent">OpenIPC {card.firmware}, {card.streamer}</p>
      <p>{t('snapshots.snapshot.uptime')}: {card.uptime}.
        {card.soc_temperature && ` ${t('snapshots.snapshot.temperature')}: ${card.soc_temperature}°C.`}</p>
      <p>{card.dimensions}, {card.bytes} bytes</p>
    </div>
  );
}

function Pagination({ page, pages, p }: { page: number; pages: number; p: Path }) {
  if (pages <= 1) return null;

  return (
    <nav class="mt-6 flex justify-center gap-2" aria-label="pagination">
      {Array.from({ length: pages }, (_, i) => i + 1).map((n) => (
        <a key={n} href={p(n === 1 ? '/open-wall' : `/open-wall/${n}`)}
           aria-current={n === page ? 'page' : undefined}
           class={`rounded border border-hairline px-3 py-1 no-underline ${
             n === page ? 'font-bold' : ''}`}>{n}</a>
      ))}
    </nav>
  );
}

function Snapshot({ data, t, p, register }: {
  data: Payload; t: Translate; p: Path;
  register: (key: string, el: HTMLCanvasElement | null) => void;
}) {
  const card = data.snapshot!;
  const strip = data.strip ?? [];

  return (
    <>
      <div class="mb-4 aspect-video bg-ink-2">
        <Frame id={card.id} variant={data.variant} alt={t('snapshots.show.snapshot_alt')}
               class="size-full object-contain" register={register} />
      </div>

      <div class="mb-4 text-sm">
        <span class="float-right text-right text-muted">
          <At at={card.at} /><br />{card.dimensions}, {card.bytes} bytes
        </span>
        <p class="mb-1 font-bold">{[card.soc, card.sensor].filter(Boolean).join(' + ').toUpperCase()}</p>
        <p class="mb-2 text-muted">OpenIPC {card.firmware}, {card.streamer}</p>
        <p>{t('snapshots.show.uptime')}: {card.uptime}.
          {card.soc_temperature && ` ${t('snapshots.show.temperature')}: ${card.soc_temperature}°C.`}</p>
        {card.caption && <p class="mt-2">{card.caption}</p>}
        {card.camera && (
          <p class="mt-2"><a href={p(`/open-wall/camera/${card.camera}`)}>
            {t('site.snapshot.link_to_camera')}</a></p>
        )}
      </div>

      {strip.length > 0 && (
        <>
          <h4 class="mt-6 mb-2 text-lg font-bold">{t('snapshots.show.last_24h')}</h4>
          <p class="mb-3">
            <a class="site-button" href={p(`/snapshots/${card.id}/oneday`)}>
              {t('snapshots.show.show_slideshow')}</a>
          </p>
          <Icons frames={strip} variant={data.strip_variant ?? 'icon2'} t={t} p={p} register={register} />
          {data.strip_more && (
            <p class="mt-3"><a href={p(`/snapshots/${card.id}/archive`)}>
              {t('snapshots.show.show_all_frames')}</a></p>
          )}
        </>
      )}
    </>
  );
}

function Icons({ frames, variant, t, p, register }: {
  frames: Icon[]; variant: FrameVariant; t: Translate; p: Path;
  register: (key: string, el: HTMLCanvasElement | null) => void;
}) {
  return (
    <div class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6">
      {frames.map((frame) => (
        <a key={frame.id} href={p(`/snapshots/${frame.id}`)}
           class="relative block overflow-hidden rounded bg-ink-2 no-underline">
          <Frame id={frame.id} variant={variant} alt={t('snapshots.icon.snapshot_alt')}
                 class="block aspect-video size-full object-cover" register={register} />
          <span class="absolute inset-x-0 bottom-0 bg-ink/70 px-1 text-center text-[.7rem] text-white">
            <At at={frame.at} />
          </span>
        </a>
      ))}
    </div>
  );
}

/**
 * The carousel, advanced by this component rather than by Bootstrap.
 *
 * The Rails page wrote every slide into the HTML and let Bootstrap show one;
 * without JavaScript that was one visible frame and ninety-five behind
 * `display: none`. Here the whole day is granted and one slide is drawn at a
 * time, which is the same picture for a reader and a great deal less markup.
 */
function Slideshow({ data, t, p, register }: {
  data: Payload; t: Translate; p: Path;
  register: (key: string, el: HTMLCanvasElement | null) => void;
}) {
  const frames = data.frames ?? [];
  const [at, setAt] = useState(0);
  const [playing, setPlaying] = useState(true);

  useEffect(() => {
    if (!playing || frames.length < 2) return undefined;
    const timer = setInterval(() => setAt((i) => (i + 1) % frames.length), 2000);
    return () => clearInterval(timer);
  }, [playing, frames.length]);

  if (frames.length === 0) return null;
  const current = frames[Math.min(at, frames.length - 1)];

  return (
    <>
      <div class="relative mb-3 aspect-video bg-ink-2">
        {frames.map((frame, i) => (
          <div key={frame.id} class="absolute inset-0" hidden={i !== at}>
            <Frame id={frame.id} variant={data.variant} alt={t('snapshots.slide.snapshot_alt')}
                   class="size-full object-contain" register={register} />
          </div>
        ))}
      </div>

      <div class="mb-3 flex flex-wrap items-center gap-3 text-sm">
        <button type="button" class="site-button" onClick={() => setPlaying((on) => !on)}>
          {playing ? '❚❚' : '▶'}
        </button>
        <input type="range" min={0} max={frames.length - 1} value={at} class="grow"
               aria-label={t('snapshots.oneday.speed')}
               onInput={(e) => { setPlaying(false); setAt(Number((e.target as HTMLInputElement).value)); }} />
        <span class="text-muted"><At at={current.at} /></span>
      </div>

      <p><a href={p(`/snapshots/${current.id}`)}>{t('snapshots.slide.link_to_this')}</a></p>
      <p class="mt-2"><a href={p(`/snapshots/${current.id}/archive`)}>
        {t('snapshots.oneday.show_all_frames')}</a></p>
    </>
  );
}

/**
 * A time the reader's own browser formats.
 *
 * Rails printed the server's UTC and left `application.js` to localise it from
 * `data-timestamp`. Here the epoch second arrives as data and the browser
 * formats it directly, which is the same answer without the second pass.
 */
function At({ at }: { at: number }) {
  const when = new Date(at * 1000);
  return <time dateTime={when.toISOString()}>{when.toLocaleString()}</time>;
}

async function paint(canvas: HTMLCanvasElement | undefined, bytes: Uint8Array): Promise<boolean> {
  if (!canvas) return false;

  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(new Blob([bytes as BlobPart], { type: 'image/jpeg' }));
  } catch {
    return false;
  }

  canvas.width = bitmap.width;
  canvas.height = bitmap.height;
  canvas.getContext('2d')?.drawImage(bitmap, 0, 0);
  bitmap.close?.();
  return true;
}

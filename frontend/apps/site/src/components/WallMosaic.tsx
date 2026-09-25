/**
 * The home page's Open Wall mosaic, with live cameras on it (#165).
 *
 * `/` is rendered by Rails and draws this out of WallHelper: a canvas per
 * camera and a signed WallGrant naming the pairs it drew. `/ru` and `/zh` are
 * prerendered, so nothing rendered them a grant, and they showed five "no
 * signal" tiles while the same page in the same language showed live cameras
 * at `/` -- reachable only by clicking your own language, which is what made
 * it look like a translation fault.
 *
 * The fix is not to put the page back in Rails. It is the shape every other
 * island here already has: the page is a file and asks an address for what it
 * cannot know at build time. `/api/v1/wall/mosaic.json` hands over the ids,
 * the captions and the grant; the frames still arrive over WallChannel,
 * masked, inside the channel's per-address budget, and no address returns an
 * image.
 *
 * Placeholders are the resting state and the failure state alike, which is
 * what home.html.erb does when its own query returns nothing: five "no signal"
 * tiles and the call to action. A reader whose socket never opens sees that
 * rather than five blanks -- but a reader behind a mirror that does not
 * forward the `Upgrade` should not be one of them, so the socket is retried
 * against the origin before the tiles give up. `requestFramesOrFallBack`.
 */
import { useEffect, useRef, useState } from 'preact/hooks';
import {
  FRAME_DEADLINE, MOSAIC_URL, captionFor, requestFramesOrFallBack, type Mosaic, type MosaicTile,
} from '../lib/wall-frames';

/** `WallHelper::FRAME_SIZES['thumb']`, so the page does not reflow when frames land. */
const THUMB = { width: 480, height: 360 };

interface Props {
  /** How many tiles the mosaic holds. */
  tiles: number;
  /** The "no signal" image, from Astro's asset pipeline. */
  placeholder: string;
  placeholderAlt: string;
  cta: string;
  ctaHref: string;
  /**
   * Where `/snapshots/<id>` lives in this page's language.
   *
   * A prefix rather than a function: island props are serialised into the page
   * and a function does not survive that.
   */
  snapshotBase: string;
  frameAlt: string;
}

export default function WallMosaic({
  tiles, placeholder, placeholderAlt, cta, ctaHref, snapshotBase, frameAlt,
}: Props) {
  const [mosaic, setMosaic] = useState<Mosaic | null>(null);
  /*
   * Which tiles have a picture on them, and whether the waiting is over.
   *
   * "No signal" is a test card, and a test card is an answer -- it says this
   * camera is not sending. It must never be the first thing a visitor sees,
   * because at that moment it is not true yet: the frames are on their way.
   * So a tile rests dark, the way the Rails page's does, and turns into the
   * card only when the answer has come in and this tile is not in it.
   *
   * Per tile, not per subscription. A frame that has been purged is delivered
   * as silence -- `WallChannel#deliver` returns without transmitting, because
   * a purged snapshot and an id that never existed must look identical -- and
   * a JPEG that will not decode is the same from here. Either leaves one tile
   * unanswered while the other four paint, and a single flag would have left
   * that one dark for ever.
   */
  const [painted, setPainted] = useState<ReadonlySet<string>>(new Set());
  const [resolved, setResolved] = useState(false);
  const canvases = useRef(new Map<string, HTMLCanvasElement>());

  useEffect(() => {
    let live = true;
    let stop: (() => void) | undefined;
    let deadline: ReturnType<typeof setTimeout> | undefined;

    (async () => {
      let loaded: Mosaic;
      try {
        const response = await fetch(MOSAIC_URL, { headers: { accept: 'application/json' } });
        if (!response.ok) throw new Error(String(response.status));
        loaded = (await response.json()) as Mosaic;
      } catch {
        // The page is correct without cameras on it -- it is the state the
        // Rails page falls back to -- so there is nothing to report and
        // nothing to retry. The tiles say so.
        if (live) setResolved(true);
        return;
      }
      if (!live) return;
      if (!loaded.grant || loaded.tiles.length === 0) { setResolved(true); return; }

      setMosaic(loaded);

      // After the render that creates the canvases, not before: `paint` needs
      // them in the map.
      queueMicrotask(() => {
        if (!live) return;
        stop = requestFramesOrFallBack({
          grant: loaded.grant!,
          requests: [{ variant: loaded.variant, ids: loaded.tiles.map((tile) => tile.id) }],
          onFrame: async (id, _variant, bytes) => {
            // Recorded only once the bitmap is actually on the canvas, so a
            // frame that will not decode counts as unanswered rather than as
            // drawn.
            if (await paint(canvases.current.get(id), bytes) && live) {
              setPainted((seen) => new Set(seen).add(id));
            }
          },
          // The channel refused, or said no. Everything still dark is dark for
          // good, so stop waiting.
          onUnavailable: () => { if (live) setResolved(true); },
        });

        // And the silent cases: a socket that never opens, and a frame that is
        // never sent. Neither produces an event, so the only way to tell them
        // from "still coming" is to stop expecting.
        deadline = setTimeout(() => { if (live) setResolved(true); }, FRAME_DEADLINE);
      });
    })();

    return () => { live = false; clearTimeout(deadline); stop?.(); };
  }, [tiles]);

  // Never more than the page has room for: the count is the server's, but the
  // layout is this page's and a mosaic of nine would break the hero's grid.
  const shown = (mosaic?.tiles ?? []).slice(0, tiles);
  // Tiles with no camera behind them. Before the answer arrives they are
  // empty; once it has, they are genuinely "no signal" -- there is no camera
  // to show, which is what home.html.erb pads its own mosaic with.
  const blanks = Math.max(0, tiles - shown.length);

  return (
    <div class="grid grid-cols-3 gap-[8px]">
      {shown.map((tile) => (
        <a
          key={tile.id}
          class="relative block overflow-hidden rounded-[.375rem] bg-ink-2 no-underline"
          href={`${snapshotBase}/${tile.id}`}
        >
          {/*
            Underneath the canvas, which is transparent until a frame is drawn
            on it -- but only once we know the frame is not coming. Until then
            the tile is the same dark rectangle the Rails page shows while its
            own canvases fill in.
          */}
          {resolved && !painted.has(tile.id) && (
            <img
              class="absolute inset-0 block size-full object-cover opacity-50"
              src={placeholder}
              alt={placeholderAlt}
              loading="lazy"
            />
          )}
          {/*
            `data-wall-frame` is how every wall check in tools/ finds a tile --
            canvas-check, wall-fills-check, bare-socket-check, the request
            trace. The Rails page carries it and this one has to as well, or a
            migrated page silently drops out of checks that still pass.
          */}
          <canvas
            ref={(el) => register(canvases.current, tile.id, el)}
            width={THUMB.width}
            height={THUMB.height}
            class="relative block aspect-video size-full object-cover"
            role="img"
            aria-label={frameAlt}
            data-wall-frame={tile.id}
            data-wall-variant={mosaic?.variant}
          />
          <Caption tile={tile} />
        </a>
      ))}

      {Array.from({ length: blanks }, (_, i) => (
        <span key={`blank-${i}`} class="relative block aspect-video overflow-hidden rounded-[.375rem] bg-ink-2">
          {(mosaic !== null || resolved) && (
            <img
              class="block aspect-video size-full object-cover opacity-50"
              src={placeholder}
              alt={placeholderAlt}
              loading="lazy"
            />
          )}
        </span>
      ))}

      {/* `.wall-tile--cta`: dashed, aspect-ratio of its own, and the only tile
          that holds text rather than a picture. */}
      <a
        class="flex aspect-video items-center justify-center rounded-[.375rem] border border-dashed
               border-white/30 bg-ink-2 p-2 text-center text-[.8125rem] text-white/70 no-underline"
        href={ctaHref}
      >
        {cta}
      </a>
    </div>
  );
}

/** `[soc, sensor]`, and nothing at all when the camera sent neither. */
function Caption({ tile }: { tile: MosaicTile }) {
  const caption = captionFor(tile);
  if (caption === '') return null;

  /* `.wall-caption`: a gradient rather than a bar, so the picture is not cut
     off by a band, and the SoC name is what it fades behind. */
  return (
    <span
      class="absolute inset-x-0 bottom-0 overflow-hidden text-ellipsis whitespace-nowrap
             bg-gradient-to-b from-transparent to-ink/85 px-2 pt-4 pb-1 font-mono
             text-[.65rem] text-white/85"
    >
      {caption}
    </span>
  );
}

function register(map: Map<string, HTMLCanvasElement>, id: string, el: HTMLCanvasElement | null) {
  if (el) map.set(id, el); else map.delete(id);
}

/**
 * A frame that will not decode leaves the canvas as it is: sized, with its
 * background, and carrying its aria-label. Better than a broken-image glyph,
 * and it is what a purged snapshot looks like.
 */
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

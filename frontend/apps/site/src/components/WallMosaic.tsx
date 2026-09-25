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
 * tiles and the call to action. A reader whose socket never opens -- behind a
 * proxy that does not forward the Upgrade -- sees that rather than five blanks.
 */
import { useEffect, useRef, useState } from 'preact/hooks';
import {
  MOSAIC_URL, captionFor, requestFrames, type Mosaic, type MosaicTile,
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
  const canvases = useRef(new Map<string, HTMLCanvasElement>());

  useEffect(() => {
    let live = true;
    let stop: (() => void) | undefined;

    (async () => {
      let loaded: Mosaic;
      try {
        const response = await fetch(MOSAIC_URL, { headers: { accept: 'application/json' } });
        if (!response.ok) throw new Error(String(response.status));
        loaded = (await response.json()) as Mosaic;
      } catch {
        // The page is correct without cameras on it -- it is the state the
        // Rails page falls back to -- so there is nothing to report and
        // nothing to retry.
        return;
      }
      if (!live || !loaded.grant || loaded.tiles.length === 0) return;

      setMosaic(loaded);

      // After the render that creates the canvases, not before: `paint` needs
      // them in the map.
      queueMicrotask(() => {
        if (!live) return;
        stop = requestFrames({
          grant: loaded.grant!,
          variant: loaded.variant,
          ids: loaded.tiles.map((tile) => tile.id),
          onFrame: (id, bytes) => paint(canvases.current.get(id), bytes),
          // Nothing to do: every tile is already showing its placeholder, and
          // an unpainted canvas is transparent, so a refusal leaves the page
          // exactly as the Rails page renders when its own query is empty.
          onUnavailable: () => {},
        });
      });
    })();

    return () => { live = false; stop?.(); };
  }, [tiles]);

  // Never more than the page has room for: the count is the server's, but the
  // layout is this page's and a mosaic of nine would break the hero's grid.
  const shown = (mosaic?.tiles ?? []).slice(0, tiles);
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
            The placeholder stays underneath, and the canvas is transparent
            until a frame is drawn on it.

            So a tile whose frame never arrives keeps its "no signal" rather
            than going empty -- and that is the ordinary case on a mirror,
            whose nginx does not forward the Upgrade, as well as what a
            refused subscription or a frame that will not decode looks like. A
            grid of blank squares is indistinguishable from a wall with no
            cameras on it, and a visitor is owed the difference.
          */}
          <img
            class="absolute inset-0 block size-full object-cover opacity-50"
            src={placeholder}
            alt={placeholderAlt}
            loading="lazy"
          />
          <canvas
            ref={(el) => register(canvases.current, tile.id, el)}
            width={THUMB.width}
            height={THUMB.height}
            class="relative block aspect-video size-full object-cover"
            role="img"
            aria-label={frameAlt}
          />
          <Caption tile={tile} />
        </a>
      ))}

      {Array.from({ length: blanks }, (_, i) => (
        <span key={`blank-${i}`} class="relative block overflow-hidden rounded-[.375rem] bg-ink-2">
          <img
            class="block aspect-video size-full object-cover opacity-50"
            src={placeholder}
            alt={placeholderAlt}
            loading="lazy"
          />
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
async function paint(canvas: HTMLCanvasElement | undefined, bytes: Uint8Array) {
  if (!canvas) return;

  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(new Blob([bytes as BlobPart], { type: 'image/jpeg' }));
  } catch {
    return;
  }

  canvas.width = bitmap.width;
  canvas.height = bitmap.height;
  canvas.getContext('2d')?.drawImage(bitmap, 0, 0);
  bitmap.close?.();
}

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
        const response = await fetch(`${MOSAIC_URL}?limit=${tiles}`, {
          headers: { accept: 'application/json' },
        });
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
          // A tile that never arrives keeps the placeholder underneath it,
          // which is the same thing an empty wall looks like.
          onUnavailable: () => {},
        });
      });
    })();

    return () => { live = false; stop?.(); };
  }, [tiles]);

  const shown = mosaic?.tiles ?? [];
  const blanks = Math.max(0, tiles - shown.length);

  return (
    <div class="grid grid-cols-3 gap-2">
      {shown.map((tile) => (
        <a
          key={tile.id}
          class="relative block aspect-video overflow-hidden rounded-md bg-ink-2 no-underline"
          href={`${snapshotBase}/${tile.id}`}
        >
          <canvas
            ref={(el) => register(canvases.current, tile.id, el)}
            width={THUMB.width}
            height={THUMB.height}
            class="size-full object-cover"
            role="img"
            aria-label={frameAlt}
          />
          <Caption tile={tile} />
        </a>
      ))}

      {Array.from({ length: blanks }, (_, i) => (
        <span key={`blank-${i}`} class="relative block aspect-video overflow-hidden rounded-md bg-ink-2">
          <img class="size-full object-cover opacity-50" src={placeholder} alt={placeholderAlt} loading="lazy" />
        </span>
      ))}

      <a
        class="flex aspect-video items-center justify-center rounded-md border border-white/20 bg-ink-2 p-2 text-center text-xs text-white no-underline hover:border-white/50"
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

  return (
    <span class="absolute inset-x-0 bottom-0 bg-ink/70 px-1.5 py-0.5 font-mono text-[10px] text-white/80">
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

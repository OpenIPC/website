import { useEffect, useState } from 'preact/hooks';

/**
 * The home page's Open Wall mosaic, filled in on load (#160).
 *
 * The Rails page read `Snapshot.latest_per_camera(limit: 5)` on every request.
 * A prerendered page cannot, so it ships the "no signal" placeholders the
 * Rails template itself falls back to when the query returns nothing, and
 * swaps in real cameras once /api/v1/wall/latest answers.
 *
 * Placeholders first and always: the tiles are in the prerendered HTML, so the
 * page is complete before any JavaScript runs and the grid never reflows from
 * nothing. A visitor with JavaScript off, or a wall with no uploads in the
 * last day, sees exactly what the Rails page shows them.
 */
interface Tile { href: string; src: string; caption: string }

export default function WallMosaic({ tiles, placeholder, placeholderAlt, cta, ctaHref }: {
  /** How many tiles the mosaic holds. */
  tiles: number;
  /** The "no signal" image, from Astro's asset pipeline. */
  placeholder: string;
  placeholderAlt: string;
  cta: string;
  ctaHref: string;
}) {
  const [cameras, setCameras] = useState<Tile[]>([]);

  useEffect(() => {
    let live = true;
    fetch('/api/v1/wall/latest', { headers: { accept: 'application/json' } })
      .then((response) => (response.ok ? response.json() : null))
      .then((data) => {
        if (!live || !data || !Array.isArray(data.snapshots)) return;
        setCameras(data.snapshots
          .filter((t: Tile) => t && typeof t.href === 'string' && typeof t.src === 'string')
          .slice(0, tiles));
      })
      // The page is correct without them, so there is nothing to report.
      .catch(() => {});
    return () => { live = false; };
  }, [tiles]);

  const blanks = Math.max(0, tiles - cameras.length);

  return (
    <div class="grid grid-cols-3 gap-2">
      {cameras.map((camera, i) => (
        <a key={camera.href} class="relative block aspect-video overflow-hidden rounded-md bg-ink-2" href={camera.href}>
          <img
            class="size-full object-cover"
            src={camera.src}
            alt={placeholderAlt}
            loading={i === 0 ? 'eager' : 'lazy'}
          />
          {camera.caption && (
            <span class="absolute right-0 bottom-0 left-0 bg-ink/70 px-1 py-0.5 text-center font-mono text-[10px] text-white">
              {camera.caption}
            </span>
          )}
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

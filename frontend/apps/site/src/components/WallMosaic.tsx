
/**
 * The home page's Open Wall mosaic (#160).
 *
 * Placeholders, and for now only placeholders -- which is what home.html.erb
 * itself falls back to when its query returns nothing, so the page is the one
 * the Rails page renders in that state rather than a new one.
 *
 * It briefly fetched /api/v1/wall/latest, an endpoint this branch added. #267
 * then landed on master and took the other direction: Open Wall frames are
 * delivered over a channel to a client that rendered the page, and no address
 * returns a camera image at all -- WallImage, Snapshot#wall_image and nginx's
 * /wall/ location are gone. An endpoint handing out image URLs would put back
 * exactly what that change removed, so it went with the rebase.
 *
 * Filling these tiles on a prerendered page now means obtaining a WallGrant
 * without Rails having rendered the page, which is a question for whoever
 * takes #165 rather than something to improvise here.
 */
export default function WallMosaic({ tiles, placeholder, placeholderAlt, cta, ctaHref }: {
  /** How many tiles the mosaic holds. */
  tiles: number;
  /** The "no signal" image, from Astro's asset pipeline. */
  placeholder: string;
  placeholderAlt: string;
  cta: string;
  ctaHref: string;
}) {
  return (
    <div class="grid grid-cols-3 gap-2">
      {Array.from({ length: tiles }, (_, i) => (
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

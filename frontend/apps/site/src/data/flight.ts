/**
 * The recorded flight on /low-latency.
 *
 * `flight-mabur-2026-10.json` is the stats.json tools/flight-ab wrote next to
 * the published streams, copied here so the page's window onto the timeline is
 * versioned with the page and cannot disagree with the streams it describes:
 * the same run produced both. A new cut is a new directory under /media/ and
 * a new copy of its stats.
 */
import stats from './flight-mabur-2026-10.json';

/**
 * The drone sits still on the ground for the first seven seconds of the
 * common window; the page starts the flight at the eighth. Trimmed here, in
 * the player's window, not in the streams, which keep the whole recording.
 */
const LEAD_IN_S = 8;

export const FLIGHT = {
  /** Where tools/flight-ab's output was uploaded, under /srv/www/shared/media/. */
  base: '/media/flights/mabur-2026-10/v2/',
  start: stats.start_s + LEAD_IN_S,
  end: stats.end_s,
};

/**
 * The R&D Player with both sides of the flight loaded into its compare.
 *
 * `/current/`, not the root: the root redirects there and drops the query
 * string, which leaves the player with an empty address box.
 */
export function rndPlayerUrl(origin: string): string {
  const mpd = (side: string) => encodeURIComponent(`${origin}${FLIGHT.base}${side}.mpd`);
  return `https://openipc.github.io/rnd-player/current/?v=${mpd('onboard')}&compare=${mpd('gs')}&t=${FLIGHT.start.toFixed(2)}`;
}

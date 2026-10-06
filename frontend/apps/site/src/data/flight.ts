/**
 * The recorded flight on /low-latency.
 *
 * `flight-mabur-2026-10.json` is the stats.json tools/flight-ab wrote next to
 * the published streams, copied here so the page's numbers are versioned with
 * the page and cannot disagree with the streams they describe: the same run
 * produced both. A new cut is a new directory under /media/ and a new copy of
 * its stats.
 */
import stats from './flight-mabur-2026-10.json';

export const FLIGHT = {
  /** Where tools/flight-ab's output was uploaded, under /srv/www/shared/media/. */
  base: '/media/flights/mabur-2026-10/v1/',
  start: stats.start_s,
  end: stats.end_s,
  /** Whole minutes, as the headline reads them. */
  minutes: Math.floor(stats.duration_s / 60),
  /** Per cent of the frames the drone sent that the pilot's screen showed. */
  shown: (stats.gs_frames_shown * 100) / stats.gs_frames_expected,
  holdsOver50: stats.gs_holds_over_50ms,
  longestHoldMs: stats.gs_longest_hold_ms,
};

/** The R&D Player with both sides of the flight loaded into its compare. */
export function rndPlayerUrl(origin: string): string {
  const mpd = (side: string) => encodeURIComponent(`${origin}${FLIGHT.base}${side}.mpd`);
  return `https://openipc.github.io/rnd-player/?v=${mpd('onboard')}&compare=${mpd('gs')}`;
}

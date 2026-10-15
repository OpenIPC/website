/**
 * A sensor name as a board, a report or a camera writes it, to the page that
 * describes the part. Only the names (src/data/sensor-names.json, exported
 * from data/sensors), so the board catalogue's islands can link a sensor
 * without carrying the sensor catalogue.
 */
import names from '../data/sensor-names.json';

const NAMES = names as Record<string, string>;

/**
 * The key a sensor name is matched by: without its maker, upper-cased. The
 * board catalogue's filter (boards/model.ts sensorKey) and the export
 * (sensorNameKey) make the same one.
 */
export function nameKey(name: string): string {
  return name.trim().replace(/^(sony|omni ?vision|onmi ?vision|silicon optronics)\s+/i, '').toUpperCase();
}

/** The urlname of the part a reported name is, or null. */
export function sensorSlug(name: string | null | undefined): string | null {
  if (!name) return null;
  return NAMES[nameKey(name)] ?? null;
}

/** Every name a part answers to, as keys: what a sensor page asks the board catalogue for. */
export function namesOf(slug: string): string[] {
  return Object.keys(NAMES).filter((k) => NAMES[k] === slug);
}

export const sensorHref = (slug: string): string => `/cameras/sensors/${slug}`;

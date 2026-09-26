/**
 * The SoC catalogue, as the hardware pages read it (#162).
 *
 * The data is data/catalogue/*.yml in the repository root -- one file per
 * vendor, reviewed in a pull request -- baked into ../data/catalogue.json by
 * `npm run export` (scripts/export-data.mjs), so that pages import JSON rather
 * than parse YAML, and export-data.test.ts fails when the two drift.
 *
 * What is NOT here is availability -- whether a visitor can generate an
 * installation guide, download firmware only, or neither. That is a question
 * about what upstream has published, answered from a release index a cron
 * refreshes hourly, so it cannot be baked into a page that may sit in a cache
 * for a week. The page states what was true when it was built and corrects
 * itself from /api/v1/hardware/availability.json on load; see
 * ../components/AvailabilityCell.tsx.
 */
import catalogue from '../data/catalogue.json';

export type Status = 'done' | 'mvp' | 'wip' | 'hlp' | 'neq' | 'rnd';

/** What a visitor can do with a chip. Soc#availability, in three states. */
export type Availability = 'wizard' | 'firmware_only' | 'none';

export interface Soc {
  model: string;
  family: string | null;
  version: string | null;
  urlname: string;
  status: Status;
  load_address: string;
  featured: boolean;
  segment: string | null;
}

export interface Vendor {
  name: string;
  urlname: string;
  full_name: string | null;
  website_url: string | null;
  socs: Soc[];
}

export const VENDORS: Vendor[] = catalogue.vendors as Vendor[];

/** Soc#full_name: the vendor's name and the model, as the row prints it. */
export function fullName(vendor: Vendor, soc: Soc): string {
  return `${vendor.name} ${soc.model}`;
}

export interface Row {
  vendor: Vendor;
  soc: Soc;
}

/** Every SoC of every vendor, ordered as `Soc.order(:name, :model)` is. */
export function allRows(): Row[] {
  return VENDORS.flatMap((vendor) => vendor.socs.map((soc) => ({ vendor, soc })));
}

export function featuredRows(): Row[] {
  return allRows().filter(({ soc }) => soc.featured);
}

export function vendorRows(urlname: string): Row[] {
  const vendor = VENDORS.find((v) => v.urlname === urlname);
  if (!vendor) throw new Error(`No vendor "${urlname}" in the catalogue.`);
  return vendor.socs.map((soc) => ({ vendor, soc }));
}

export function vendorBySlug(urlname: string): Vendor {
  const vendor = VENDORS.find((v) => v.urlname === urlname);
  if (!vendor) throw new Error(`No vendor "${urlname}" in the catalogue.`);
  return vendor;
}

/** The address of a vendor's tab, which is a Rails route until #163. */
export function vendorPath(vendor: Vendor): string {
  return `/cameras/vendors/${vendor.urlname}`;
}

/** The address of one SoC's wizard, which stays on Rails until #163/#164. */
export function socPath(vendor: Vendor, soc: Soc): string {
  return `/cameras/vendors/${vendor.urlname}/socs/${soc.urlname}`;
}

export const STATUSES: Status[] = ['neq', 'rnd', 'hlp', 'wip', 'mvp', 'done'];

export const SOC_COUNT = allRows().length;

export const VENDOR_NAMES = VENDORS.map((v) => v.name);

/**
 * The two catalogue figures the home page states (#160, #161).
 *
 * `PagesController#home` read these from the database on every request:
 * `Soc.count`, and `Vendor.soc_vendors.order(:name).pluck(:name)`.
 *
 * They were baked by hand, with `bin/rails catalogue:check` as the guard
 * against their going stale. #161 removed the need for that: the catalogue is
 * data/catalogue/*.yml now, baked into ./catalogue.json by `bin/rails
 * catalogue:bake`, so these are a function of the same tree every other page
 * is built from and cannot disagree with it.
 *
 * Every vendor in the catalogue has SoCs, which is what `soc_vendors` means --
 * the table also holds sensor makers, and they are not in these files.
 */
import { SOC_COUNT as COUNT, VENDOR_NAMES } from '../lib/hardware';

export const SOC_COUNT = COUNT;

export const SOC_VENDOR_NAMES = VENDOR_NAMES;

/**
 * The two catalogue figures the home page states (#160).
 *
 * `PagesController#home` read these from the database on every request:
 * `Soc.count`, and `Vendor.soc_vendors.order(:name).pluck(:name)` -- soc
 * vendors rather than every Vendor, because the table also holds sensor makers
 * and counting them as silicon we run on overstates the list.
 *
 * A prerendered page cannot read a database, so they are baked. Baked numbers
 * go stale silently, which is the whole objection to baking them, and the
 * guard is `bin/rails catalogue:check` -- a task rather than a test, because
 * these figures come from the production catalogue and CI's database is empty,
 * so a test would compare 126 against 0 and fail on every run. Run it where
 * the data is:
 *
 *     docker exec openipc-web-prod bin/rails catalogue:check
 *
 * #161 moves the catalogue into git-versioned YAML and this file goes with it:
 * the numbers become a function of the same tree the build already reads, and
 * the task can be deleted rather than kept running.
 *
 * Read from production on 2026-09-22.
 */
export const SOC_COUNT = 126;

export const SOC_VENDOR_NAMES = [
  'Allwinner',
  'Ambarella',
  'Anyka',
  'Fullhan',
  'Goke',
  'GrainMedia',
  'HiSilicon',
  'Ingenic',
  'MStar',
  'Novatek',
  'Rockchip',
  'SigmaStar',
  'TI',
  'Xiongmai',
] as const;

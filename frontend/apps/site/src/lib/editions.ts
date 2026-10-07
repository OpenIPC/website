/**
 * The editions OpenIPC/builder builds rather than OpenIPC/firmware: the FPV
 * stacks (#390). The firmware role offers them from builder's releases
 * (service/internal/firmware FPVEditions holds the same list). `wfbng` is the
 * wfb-ng air unit, called `fpv` until builder renamed it; both are accepted
 * while cameras in the field still report the old name.
 */
export const BUILDER_EDITIONS = ['wfbng', 'fpv', 'waybeam', 'rubyfpv', 'apfpv'] as const;

export function fromBuilder(edition: string): boolean {
  return (BUILDER_EDITIONS as readonly string[]).includes(edition);
}

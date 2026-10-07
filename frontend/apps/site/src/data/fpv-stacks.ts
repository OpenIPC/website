/**
 * The FPV stacks /low-latency offers to try (#390), each an edition the
 * installer takes from OpenIPC/builder (../lib/editions.ts).
 *
 * `socs` is what builder published, and the installer could install, when
 * the page was built (2026-10-07). It is only the first paint: the page
 * corrects it on load from /api/v1/hardware/availability.json, whose `fpv`
 * map is the same answer, live. A stack with nothing there yet says so rather
 * than linking to an installer that cannot offer it.
 */
export interface FpvStack {
  /** Locale keys are pages.low_latency.build_<key>_name / _text. */
  key: string;
  /** The editions that are this stack, the current name first. */
  editions: string[];
  upstream: string;
  /** SoC slug to the edition it installs this stack as. */
  socs: Record<string, string>;
}

export const FPV_STACKS: FpvStack[] = [
  {
    key: 'wfbng',
    editions: ['wfbng', 'fpv'],
    upstream: 'https://github.com/svpcom/wfb-ng',
    socs: Object.fromEntries(['ssc338q', 'ssc30kq', 'ssc378qe', 'gk7205v200', 'gk7205v210', 'gk7205v300',
      'hi3516ev200', 'hi3516ev300'].map((s) => [s, 'fpv'])),
  },
  {
    key: 'waybeam',
    editions: ['waybeam'],
    upstream: 'https://github.com/OpenIPC/waybeam',
    socs: {},
  },
  {
    key: 'rubyfpv',
    editions: ['rubyfpv'],
    upstream: 'https://rubyfpv.com',
    socs: { ssc338q: 'rubyfpv', ssc30kq: 'rubyfpv' },
  },
  {
    key: 'apfpv',
    editions: ['apfpv'],
    upstream: 'https://github.com/OpenIPC/builder/tree/master/devices/apfpv',
    socs: { ssc338q: 'apfpv', ssc378qe: 'apfpv' },
  },
];

/** The edition a SoC installs a stack as: the first of its editions the SoC has. */
export function editionOn(stack: FpvStack, published: string[]): string | null {
  return stack.editions.find((e) => published.includes(e)) ?? null;
}

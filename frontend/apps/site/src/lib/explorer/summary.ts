/**
 * The summary's arithmetic: the flash map and the headroom states. Pure, so
 * the page and its tests agree on what a build's chip looks like.
 */
import type { Sizes } from "./types";

/** U-Boot and its environment: 0x0–0x50000 on every NOR layout the site offers. */
export const BOOT_KB = 320;

export type SegmentKind = "boot" | "kernel" | "rootfs" | "overlay";

export type Segment = {
  kind: SegmentKind;
  /** Offset from the start of the chip, in KiB. */
  offsetKb: number;
  sizeKb: number;
  /** What the build uses of it, for the kernel and root filesystem. */
  usedKb: number | null;
};

/**
 * The chip as the build lays it out: boot, kernel cap, rootfs cap, and the
 * overlay in whatever is left. Null when the report does not say how big the
 * chip is or where its caps are -- the page then shows the meters alone.
 */
export function flashSegments(s: Sizes): Segment[] | null {
  const k = s.headroom?.kernel;
  const r = s.headroom?.rootfs;
  if (!s.flash_mb || !k?.cap_kb || !r?.cap_kb) return null;
  const total = s.flash_mb * 1024;
  const overlay = total - BOOT_KB - k.cap_kb - r.cap_kb;
  if (overlay < 0) return null;
  const segs: Segment[] = [
    { kind: "boot", offsetKb: 0, sizeKb: BOOT_KB, usedKb: null },
    { kind: "kernel", offsetKb: BOOT_KB, sizeKb: k.cap_kb, usedKb: k.used_kb },
    { kind: "rootfs", offsetKb: BOOT_KB + k.cap_kb, sizeKb: r.cap_kb, usedKb: r.used_kb },
    { kind: "overlay", offsetKb: BOOT_KB + k.cap_kb + r.cap_kb, sizeKb: overlay, usedKb: null },
  ];
  return segs;
}

/** "0x250000" -- an offset as a bootloader command would write it. */
export function hexOffset(kb: number): string {
  return "0x" + (kb * 1024).toString(16).toUpperCase();
}

export type HeadroomState = "ok" | "warn" | "crit" | "over" | "unknown";

/** Under 10% free is tight, under 2% almost full, below zero over the cap. */
export function headroomState(usedKb: number | null | undefined, capKb: number | null | undefined): HeadroomState {
  if (usedKb == null || !capKb) return "unknown";
  const free = capKb - usedKb;
  if (free < 0) return "over";
  const pct = (free / capKb) * 100;
  if (pct < 2) return "crit";
  if (pct < 10) return "warn";
  return "ok";
}

/** Does this build need more than an 8 MB chip? (#285) */
export function needsMoreThanEight(s: Sizes): boolean {
  return (s.flash_mb ?? 0) > 8;
}

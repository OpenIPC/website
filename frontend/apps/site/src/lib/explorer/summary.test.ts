import { describe, expect, it } from "vitest";
import { flashSegments, headroomState, hexOffset, needsMoreThanEight } from "./summary";
import type { Sizes } from "./types";

function sizes(flash: number | null, kernel: [number, number], rootfs: [number, number]): Sizes {
  return {
    schema: 1, board: "b", variant: "lite", flash_mb: flash, kernel_version: "4.9.37",
    rootfs: { uncompressed_bytes: 1, compressed_bytes: 1, compression: "xz", compression_ratio: 0.4 },
    kernel: { image_path: null, uimage_bytes: 1, vmlinux_bytes: 1 },
    headroom: {
      kernel: { used_kb: kernel[0], cap_kb: kernel[1], headroom_kb: kernel[1] - kernel[0] },
      rootfs: { used_kb: rootfs[0], cap_kb: rootfs[1], headroom_kb: rootfs[1] - rootfs[0] },
    },
    packages: [], linux_components: { kernel_image: { image_path: null, uimage_bytes: 1, vmlinux_bytes: 1 }, modules: [], built_in: [], autoload_list: [] },
    removed_by_finalize: [],
  };
}

describe("flashSegments", () => {
  it("lays out an 8 MB chip at the offsets the installer uses", () => {
    // gk7205v200-lite, 2026-09-25.
    const segs = flashSegments(sizes(8, [1877, 2048], [5116, 5120]))!;
    expect(segs.map((s) => [s.kind, hexOffset(s.offsetKb), s.sizeKb])).toEqual([
      ["boot", "0x0", 320],
      ["kernel", "0x50000", 2048],
      ["rootfs", "0x250000", 5120],
      ["overlay", "0x750000", 704],
    ]);
    expect(segs[2].usedKb).toBe(5116);
  });

  it("gives a 16 MB build its larger rootfs cap", () => {
    // hi3516cv500-lite: made for 16 MB only (#285).
    const segs = flashSegments(sizes(16, [1943, 2048], [7864, 8192]))!;
    expect(segs[3]).toMatchObject({ kind: "overlay", offsetKb: 320 + 2048 + 8192, sizeKb: 16384 - 320 - 2048 - 8192 });
  });

  it("draws nothing it cannot measure", () => {
    expect(flashSegments(sizes(null, [1, 2048], [1, 5120]))).toBeNull();
    expect(flashSegments(sizes(8, [1, 0], [1, 5120]))).toBeNull();
    // Caps that do not fit the chip are a report problem, not a map.
    expect(flashSegments(sizes(8, [1, 4096], [1, 8192]))).toBeNull();
  });
});

describe("headroomState", () => {
  it.each([
    [1000, 2048, "ok"],
    [1877, 2048, "warn"], // 8.3% free
    [5116, 5120, "crit"], // 0.08% free
    [2100, 2048, "over"],
    [null, 2048, "unknown"],
    [10, 0, "unknown"],
  ] as const)("%s of %s KiB is %s", (used, cap, want) => {
    expect(headroomState(used, cap)).toBe(want);
  });
});

describe("needsMoreThanEight", () => {
  it("is the #285 case", () => {
    expect(needsMoreThanEight(sizes(16, [1, 2048], [7864, 8192]))).toBe(true);
    expect(needsMoreThanEight(sizes(8, [1, 2048], [1, 5120]))).toBe(false);
    expect(needsMoreThanEight(sizes(null, [1, 2048], [1, 5120]))).toBe(false);
  });
});

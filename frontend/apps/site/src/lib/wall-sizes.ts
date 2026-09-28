/**
 * The wall variants' pixel sizes, so a canvas is the shape of the frame it will
 * hold and the page does not reflow when the bytes arrive (#165).
 *
 * One copy here, because four views draw frames and a second would drift the
 * day a variant is resized. The server no longer resizes anything -- it keeps
 * what the camera sent -- so these are the boxes a picture is fitted into
 * when it is painted (wall-decode.ts), not the size of what arrives.
 */
export const FRAME_SIZES = {
  icon: { width: 90, height: 60 },
  icon2: { width: 240, height: 135 },
  thumb: { width: 480, height: 360 },
  fullhd: { width: 1920, height: 1080 },
} as const;

export type FrameVariant = keyof typeof FRAME_SIZES;

/** The box a frame asked for at `variant` is painted into. */
export function boxFor(variant: string): { width: number; height: number } {
  return FRAME_SIZES[variant as FrameVariant] ?? FRAME_SIZES.fullhd;
}

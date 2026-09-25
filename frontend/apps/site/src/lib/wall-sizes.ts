/**
 * `WallHelper::FRAME_SIZES`, so a canvas is the shape of the frame it will
 * hold and the page does not reflow when the bytes arrive (#165).
 *
 * One copy here, because four views draw frames and a second would drift the
 * day a variant is resized. `test/helpers/wall_helper_test.rb` asserts this
 * file against the Ruby rather than trusting the comment.
 */
export const FRAME_SIZES = {
  icon: { width: 90, height: 60 },
  icon2: { width: 240, height: 135 },
  thumb: { width: 480, height: 360 },
  fullhd: { width: 1920, height: 1080 },
} as const;

export type FrameVariant = keyof typeof FRAME_SIZES;

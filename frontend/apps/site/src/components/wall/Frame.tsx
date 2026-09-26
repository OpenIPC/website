/**
 * One camera frame: a canvas, and nothing that is an address (#165).
 *
 * `data-wall-frame` is how every wall check in tools/ finds a tile, and the
 * wall's pages have always carried it. The canvas is sized to the variant so
 * the page does not reflow when the bytes land, and it stays blank rather
 * than showing a broken-image glyph when they do not -- which is what a purged
 * snapshot looks like from here.
 */
import { FRAME_SIZES, type FrameVariant } from '../../lib/wall-sizes';

interface Props {
  id: string;
  variant: FrameVariant;
  alt: string;
  class?: string;
  register: (key: string, el: HTMLCanvasElement | null) => void;
}

export default function Frame({ id, variant, alt, class: className, register }: Props) {
  const { width, height } = FRAME_SIZES[variant];

  return (
    <canvas
      ref={(el) => register(`${id}:${variant}`, el)}
      width={width}
      height={height}
      class={className}
      role="img"
      aria-label={alt}
      data-wall-frame={id}
      data-wall-variant={variant}
    />
  );
}

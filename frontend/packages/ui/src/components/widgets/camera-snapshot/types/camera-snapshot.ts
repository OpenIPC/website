import type { VNode } from 'preact';

export type CamData = {
  soc: string,
  date: string,
  firmware: string,
  /** Seconds. Rendered as `2d 5h` / `5h 12m` / `12m` by formatUptime(). */
  uptime: number,
  /** Degrees Celsius. Omitted when the camera does not report one. */
  socTemp?: number,
  resolution: string,
  /** Bytes. */
  size: number,
}

export type CameraSnapshotProps = CamData & {
  /** Renders the skeleton instead of the tile. */
  loading?: boolean,
  /** The still the camera uploaded. */
  imageUrl?: string,
  alt?: string,
  /**
   * Anything that should occupy the media box instead of the still -- a live
   * player, for instance. The package deliberately knows nothing about HLS:
   * fancyweb-ng hard-coded http://localhost:4000/index.m3u8 here, and the Open
   * Wall (#165) serves stills over JSON, not video.
   */
  media?: VNode,
}

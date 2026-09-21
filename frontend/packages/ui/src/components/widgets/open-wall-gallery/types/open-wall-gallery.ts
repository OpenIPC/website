import type { CameraSnapshotProps } from '../../camera-snapshot/types';

export type OpenWallGalleryProps = {
  /** One tile per camera, already fetched by the host. */
  cameras: (CameraSnapshotProps & { id: string })[],
};

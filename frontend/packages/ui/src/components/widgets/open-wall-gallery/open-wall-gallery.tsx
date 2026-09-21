import CameraSnapshot from '../camera-snapshot/camera-snapshot';
import type { OpenWallGalleryProps } from './types/open-wall-gallery';

export default function OpenWallGallery({ cameras }: OpenWallGalleryProps) {
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-5">
      {cameras.map(({ id, ...camera }) => <CameraSnapshot key={id} {...camera} />)}
    </div>
  );
}

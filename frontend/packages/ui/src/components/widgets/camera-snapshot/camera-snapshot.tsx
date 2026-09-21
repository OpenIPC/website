import type { CameraSnapshotProps } from './types';
import UIIcons from '../../../assets/icons/ui';
import { formatUptime } from './format-uptime';

const tooltip = 'relative hover:after:absolute hover:after:z-10 hover:after:rounded '
  + 'hover:after:border hover:after:bg-white hover:after:p-1 hover:after:text-xs '
  + 'hover:after:text-nowrap hover:after:content-[attr(data-descr)]';

function Skeleton() {
  const { CameraPreloader } = UIIcons;
  return (
    <>
      <div className="relative aspect-video min-w-40 bg-light-grey">
        <div className="
          absolute inset-0 z-10 flex flex-row items-center justify-center
          *:size-12
        ">
          <CameraPreloader />
        </div>
      </div>
      <div className="animate-pulse p-2 pt-4">
        <div className="flex flex-row flex-nowrap justify-between gap-x-2">
          <div className="h-[20px] w-[48%] rounded-sm bg-wallet-border"></div>
          <div className="h-[20px] w-[48%] rounded-sm bg-wallet-border"></div>
        </div>
        <div className="mt-[2px] h-[14px] w-[48%] rounded-sm bg-wallet-border"></div>
        <div className="my-[8px] h-[20px] w-[48%] rounded-sm bg-wallet-border"></div>
        <div className="h-[20px] w-[48%] rounded-sm bg-wallet-border"></div>
      </div>
    </>
  );
}

export default function CameraSnapshot(props: CameraSnapshotProps) {
  const {
    loading = false, imageUrl, alt = '', media,
    soc, date, firmware, uptime, socTemp, resolution, size,
  } = props;

  if (loading) {
    return (
      <div className="rounded-md border border-wallet-border">
        <Skeleton />
      </div>
    );
  }

  const uptimeLabel = formatUptime(uptime);

  return (
    <div className="rounded-md border border-wallet-border">
      <div className="aspect-video min-w-40">
        {media ?? (
          imageUrl
            ? <img src={imageUrl} alt={alt} className="
              aspect-video w-full min-w-40 object-cover
            " />
            : <div className="aspect-video w-full min-w-40 bg-light-grey"></div>
        )}
      </div>
      <div className="p-2 pt-4">
        <div className="flex flex-row flex-nowrap justify-between gap-x-4">
          <div className={`
            max-w-[48%]
            hover:after:-top-8 hover:after:left-2 hover:after:font-bold
            ${tooltip}
          `} data-descr={soc}>
            <p className="w-full truncate text-sm font-bold text-nowrap">{soc}</p>
          </div>
          <div className={`
            max-w-[48%]
            hover:after:-top-8 hover:after:right-2
            ${tooltip}
          `} data-descr={date}>
            <p className="w-full truncate text-sm text-nowrap">{date}</p>
          </div>
        </div>
        <p className="text-xs text-brand-blue">{firmware}</p>
        <div className="
          flex max-w-max flex-row flex-nowrap justify-start gap-x-1 py-2
          text-nowrap
        ">
          {uptimeLabel && <p className="text-sm">Uptime: {uptimeLabel}</p>}
          {socTemp !== undefined
            && <p className="truncate text-sm">{uptimeLabel && ', '}SoC temperature: {socTemp} &deg;C</p>}
        </div>
        <p className="text-sm">{resolution}, {size} bytes</p>
      </div>
    </div>
  );
}

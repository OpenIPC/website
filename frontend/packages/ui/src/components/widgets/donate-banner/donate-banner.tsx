import { DonateBannerProps } from './types';
import { donateBannerConstants } from './constants';
import  UIIcons from '../../../assets/icons/ui';
import { JSX } from 'preact/jsx-runtime';

export default function DonateBanner(props: DonateBannerProps) {
  const { size } = props;
  const { Coin, OpenCollective } = UIIcons;

  type Sizes = DonateBannerProps['size'];

  function getProperBannerSize(d: Sizes):JSX.Element {
    const bannerShop: Record<Sizes, () => JSX.Element> = {
      'small': () => (
        <a className="block" href={donateBannerConstants.small.link.href}>
          <div className="
            flex max-w-80 flex-row items-center justify-between rounded-full
            bg-linear-to-t from-opencol-donban-bg-from to-opencol-donban-bg-to
            p-0.5 font-medium text-white
          ">
              <div className="
                flex shrink grow flex-row items-center justify-center
              ">
                <p className="text-center text-[13px] tracking-widest">{donateBannerConstants.small.text}</p>
              </div> 
              <div className="
                flex size-12 shrink-0 grow-0 flex-col items-center
                justify-center rounded-full bg-white
                *:size-9
              ">
                <OpenCollective />
              </div>
          </div>
        </a>
      ),
      'big': () => (
        <div className="
          flex w-full min-w-fit flex-row rounded-md border border-light-blue
          bg-donban-bg p-1
        ">
          <div className="
            flex max-w-16 basis-2/12 flex-row items-center justify-center
            *:h-[clamp(36px,11vw,50px)] *:w-[clamp(36px,11vw,60px)]
          ">
            <Coin />
          </div>
          <div className="
            flex basis-10/12 flex-col gap-y-0.5 p-1 text-action-blue
          ">
            <p className="text-[clamp(12px,6vw,24px)] font-normal">{donateBannerConstants.big.text[0]}</p>
            <p className="text-xs">
              <a className="
                text-[clamp(10px,3.5vw,18px)] text-brand-blue underline
              " href={donateBannerConstants.big.link.href}>{donateBannerConstants.big.link.text}</a>
              {donateBannerConstants.big.text[1]}
            </p>
          </div>
        </div>
      ),
    };
    return bannerShop[d]();
  };

  return getProperBannerSize(size);
}

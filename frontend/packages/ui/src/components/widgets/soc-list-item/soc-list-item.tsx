import type { SoCItem } from '../soc-managed-list/types';

export type SoCListItemProps = SoCItem & {
  /**
   * Where this SoC's installation page lives. Given by the caller, because
   * only the caller knows the route: openipc.org addresses SoCs by the
   * `urlname` slug its models generate, which is not the vendor and model as
   * displayed. Left out, the cell states support without linking.
   *
   * fancyweb-ng read window.location during render and appended the display
   * values -- unusable in a server render, and a 404 against the real route.
   */
  href?: string,
};
import { SoCItemSpecificConstants, installationAlternatives } from './constants';
import SoCIcons from '../../../assets/icons/socs-info';

export default function SoCListItem(props: SoCListItemProps) {
  const { vendor, model, address, stage, firmware, href } = props;
  const StageIcon = SoCIcons[stage];

  return (
    <li className="
      grid grid-cols-5 grid-rows-4 flex-row rounded-sm border
      border-wallet-border
      md:flex md:min-h-11 md:flex-nowrap md:gap-x-1 md:border-0
    ">
      <p className="
        col-start-1 col-end-4 row-start-1 row-end-2 mr-1 bg-wallet-bg pt-1
        text-center font-medium
        md:hidden
      ">
        {SoCItemSpecificConstants.SoCCellTitle}
      </p>
      <p className="
        col-start-1 col-end-4 row-start-2 row-end-3 mr-1 mb-1 bg-wallet-bg pb-1
        text-center
        md:m-0 md:min-w-36 md:shrink-0 md:grow-3 md:basis-0 md:content-center
        md:p-0 md:pl-2 md:text-left
      ">
        {`${vendor} ${model}`}
      </p>
      <p className="
        col-start-4 col-end-6 row-start-1 row-end-2 bg-wallet-bg pt-1
        text-center font-medium
        md:hidden
      ">
        {SoCItemSpecificConstants.addressCellTitle}
      </p>
      <p className="
        col-start-4 col-end-6 row-start-2 row-end-3 mb-1 bg-wallet-bg pb-1
        text-center
        md:m-0 md:min-w-32 md:shrink-0 md:grow-2 md:basis-0 md:content-center
        md:p-0 md:pl-2 md:text-left
      ">
        {address}
      </p>
      <p className="
        col-start-1 col-end-2 row-start-3 row-end-4 mr-1 bg-wallet-bg pt-1
        text-center font-medium
        md:hidden
      ">
        {SoCItemSpecificConstants.stageCellTitle}
      </p>
      <p className="
        col-start-1 col-end-2 row-start-4 row-end-5 mr-1 flex flex-row
        justify-center bg-wallet-bg pb-1
        *:h-[22px] *:w-[33px]
        md:m-0 md:min-w-14 md:shrink-0 md:grow md:basis-0 md:flex-col
        md:items-center md:p-0
      ">
        <StageIcon />
      </p>
      <p className="
        col-start-2 col-end-6 row-start-3 row-end-4 bg-wallet-bg pt-1
        text-center font-medium
        md:hidden
      ">
        {SoCItemSpecificConstants.installationCellTitle}
      </p>
      <p className="
        col-start-2 col-end-6 row-start-4 row-end-5 bg-wallet-bg pb-1
        text-center
        md:min-w-56 md:shrink-0 md:grow-4 md:basis-0 md:content-center md:p-0
        md:pl-2 md:text-left
      ">
        {href && firmware.length > 0 && address !== null && address.length > 0
          ? <a className="
            text-brand-blue
            hover:text-btn-blue-hover
          " href={href}>{installationAlternatives.yes}</a>
          : installationAlternatives.no
        }
      </p>
    </li>
  )
}

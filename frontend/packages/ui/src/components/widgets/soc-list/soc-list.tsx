import { SoCManagedListProps } from '../soc-managed-list/types';
import { SoCItemSpecificConstants } from '../soc-list-item/constants';
import SoCListItem from '../soc-list-item/soc-list-item';

type SoCListProps = {
  list: SoCManagedListProps['fullList'],
  /**
   * Builds each row's installation link. Without it the rows say what is
   * supported without linking anywhere, which is the honest default for a
   * package that does not know the host's routes.
   */
  hrefFor?: (soc: SoCManagedListProps['fullList'][number]) => string | undefined,
}

export default function SoCList(props: SoCListProps) {
  const { list, hrefFor } = props;

  return (
    <ul className="flex flex-col gap-y-2 self-stretch">
      <li className="
        hidden flex-row font-medium
        md:flex md:min-h-11 md:flex-nowrap md:gap-x-1 md:border-0
      ">
        <p className="
          col-start-1 col-end-4 row-start-2 row-end-3 mr-1 mb-1 bg-wallet-bg
          pb-1 text-center
          md:m-0 md:min-w-36 md:shrink-0 md:grow-3 md:basis-0 md:content-center
          md:p-0 md:pl-2 md:text-left
        ">
          {SoCItemSpecificConstants.SoCCellTitle}
        </p>
        <p className="
          col-start-4 col-end-6 row-start-2 row-end-3 mb-1 bg-wallet-bg pb-1
          text-center
          md:m-0 md:min-w-32 md:shrink-0 md:grow-2 md:basis-0 md:content-center
          md:p-0 md:pl-2 md:text-left
        ">
          {SoCItemSpecificConstants.addressCellTitle}
        </p>
        <p className="
          col-start-1 col-end-2 row-start-4 row-end-5 mr-1 flex flex-row
          justify-center bg-wallet-bg pb-1
          *:h-[22px] *:w-[33px]
          md:m-0 md:min-w-14 md:shrink-0 md:grow md:basis-0 md:flex-col
          md:items-center md:p-0
        ">
          {SoCItemSpecificConstants.stageCellTitle}
        </p>
        <p className="
          col-start-2 col-end-6 row-start-4 row-end-5 bg-wallet-bg pb-1
          text-center
          md:min-w-56 md:shrink-0 md:grow-4 md:basis-0 md:content-center md:p-0
          md:pl-2 md:text-left
        ">
          {SoCItemSpecificConstants.installationCellTitle}
        </p>
      </li>
      { list.map(item => <SoCListItem key={`${item.vendor} ${item.model}`} {...item} href={hrefFor?.(item)} />) }
    </ul>
  );
}

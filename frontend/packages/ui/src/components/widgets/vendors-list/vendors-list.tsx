import { vendorsListProps } from './types';

export default function VendorsList(props: vendorsListProps) {
  const { list, curSelected, clickHandler } = props;

  const isLastElem = (cur: number, arr: typeof list) => cur === arr.length - 1;

  const handleClick = (e: MouseEvent) => {
    const letterContainer = e.currentTarget;
    if (letterContainer instanceof HTMLLIElement) {
      const vendor = letterContainer!.getElementsByTagName('span')[0].innerText;
      if (curSelected?.toLowerCase() === vendor.toLowerCase()) return;
      clickHandler(vendor);
    }
  }

  return (
    <div className="max-w-full overflow-x-auto">
      <ul className="flex flex-row flex-nowrap pb-2 text-sm text-text-blue">
        {list.map((vendor: string, i: number) => (
          <li key={vendor} className={`
            relative rounded-t border border-transparent border-b-grey p-[6px]
            text-nowrap
            ${curSelected && vendor.toLowerCase() === curSelected.toLowerCase() && `
              border-x-grey border-t-grey border-b-white
              *:border-0
            `}
            hover:cursor-pointer hover:border-x-grey hover:border-t-grey
            hover:border-b-white hover:text-btn-blue-hover
            *:hover:border-0
          `} onClick={handleClick}>
            {!isLastElem(i, list) && <div className="
              absolute right-[-2px] -bottom-px h-[2px] w-[3px] border
              border-transparent border-b-grey
            "></div>}
            <span>{vendor}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

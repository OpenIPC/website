import { vendorsListProps } from './types';

export default function VendorsList(props: vendorsListProps) {
  const { list, curSelected, clickHandler } = props;

  const isLastElem = (cur: number, arr: typeof list) => cur === arr.length - 1;

  /**
   * Keyboard-operable, and told which vendor it is rather than reading the
   * label back out of the DOM. See the note in AbcSelector.
   */
  const tabProps = (vendor: string) => {
    const selected = curSelected?.toLowerCase() === vendor.toLowerCase();
    const choose = () => { if (!selected) clickHandler(vendor); };
    return {
      role: 'tab' as const,
      tabIndex: 0,
      'aria-selected': selected,
      onClick: choose,
      onKeyDown: (e: KeyboardEvent) => {
        if (e.key !== 'Enter' && e.key !== ' ') return;
        e.preventDefault();
        choose();
      },
    };
  };

  return (
    <div className="max-w-full overflow-x-auto">
      <ul role="tablist" aria-label="Filter by vendor" className="
        flex flex-row flex-nowrap pb-2 text-sm text-text-blue
      ">
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
          `} {...tabProps(vendor)}>
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

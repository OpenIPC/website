import { AbcSelectorProps } from './types';

export default function AbcSelector(props: AbcSelectorProps) {
  const { letters, curSelected, clickHandler } = props;

  const isLastElem = (cur: number, arr: typeof letters) => {
    return cur === arr.length - 1;
  };

  /**
   * Each tab is operable from the keyboard and reports which one is current.
   *
   * These were list items with an onClick and nothing else: not focusable,
   * not announced, and unreachable without a pointer -- which made the whole
   * hardware catalogue unfilterable for a keyboard user. The handler also
   * used to read the label back out of the DOM with innerText rather than
   * being told what was clicked.
   */
  const tabProps = (label: string) => ({
    role: 'tab' as const,
    tabIndex: 0,
    'aria-selected': curSelected?.toLowerCase() === label.toLowerCase(),
    onClick: () => clickHandler(label),
    onKeyDown: (e: KeyboardEvent) => {
      if (e.key !== 'Enter' && e.key !== ' ') return;
      e.preventDefault();
      clickHandler(label);
    },
  });

  return (
    <div className="max-w-full overflow-x-auto">
      <ul role="tablist" aria-label="Filter by first letter" className="
        box-border flex flex-row flex-nowrap overflow-x-auto pb-2 text-sm
        text-text-blue
      ">
        <li className={`
          relative rounded-t border border-transparent border-b-grey p-[7px]
          ${curSelected && 'Recommended'.toLowerCase() === curSelected!.toLowerCase() && `
            border-x-grey border-t-grey border-b-white
            *:border-0
          `}
          hover:cursor-pointer hover:border-x-grey hover:border-t-grey
          hover:border-b-white hover:text-btn-blue-hover
          *:hover:border-0
        `} {...tabProps('Recommended')}>
          <div className="
            absolute right-[-2px] -bottom-px h-[2px] w-[3px] border
            border-transparent border-b-grey
          "></div>
          <span>Recommended</span>
        </li>
        <li className={`
          relative rounded-t border border-transparent border-b-grey p-[7px]
          text-nowrap
          ${curSelected && 'Full list'.toLowerCase() === curSelected!.toLowerCase() && `
            border-x-grey border-t-grey border-b-white
            *:border-0
          `}
          hover:cursor-pointer hover:border-x-grey hover:border-t-grey
          hover:border-b-white hover:text-btn-blue-hover
          *:hover:border-0
        `} {...tabProps('Full list')}>
          <div className="
            absolute right-[-2px] -bottom-px h-[2px] w-[3px] border
            border-transparent border-b-grey
          "></div>
          <span>Full list</span>
        </li>
        {letters.map((letter: string, i: number) => (
          <li key={letter} className={`
            relative rounded-t border border-transparent border-b-grey px-[12px]
            py-[7px] text-nowrap
            ${curSelected && letter.toLowerCase() === curSelected!.toLowerCase() && `
              border-x-grey border-t-grey border-b-white
              *:border-0
            `}
            hover:cursor-pointer hover:border-x-grey hover:border-t-grey
            hover:border-b-white hover:text-btn-blue-hover
            *:hover:border-0
          `} {...tabProps(letter)}>
            {!isLastElem(i, letters) && <div className="
              absolute right-[-2px] -bottom-px h-[2px] w-[3px] border
              border-transparent border-b-grey
            "></div>}
            <span>{letter}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

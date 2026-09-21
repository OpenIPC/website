import {RefObject} from 'preact';
import type { MenuItems } from '../../../../Header-menu';
import MenuItem from '../menu-item';
import { useLayoutEffect, useRef } from 'preact/hooks';

interface SubMenuProps {
  level: number;
  parent?: RefObject<HTMLLIElement>;
  menuItems: MenuItems;
  menuItemClickHandler: () => void;
}

export default function SubMenu({
  level,
  parent,
  menuItems,
  menuItemClickHandler,
}: SubMenuProps) {

  const divRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    if (level === 3 && parent && parent.current && divRef.current) {
      const parentBoundingRect = parent.current.getBoundingClientRect();
      const divBoundingRect = divRef.current.getBoundingClientRect();
      const { right: parentRight } = parentBoundingRect;
      const { width: divWidth } = divBoundingRect;
      const freeRightSpace = window.innerWidth - parentRight;
      if (divWidth > freeRightSpace) {
        divRef.current.style.left = '-240px';
        divRef.current.style.top = '-42px';
      } else {
        divRef.current.style.left = '158px';
        divRef.current.style.top = '-42px';
      }
    }
  }, [level, parent]);
  
  return (
    <div
      className="
        absolute top-[10px] right-[-22px] m-6 w-50 border border-light-blue
        bg-brand-blue
      "
      ref={divRef}
    >
      { level === 2 && (
        <div className={`
          absolute top-[-9px] right-6 size-4 rotate-45 border-t border-l
          border-white bg-brand-blue
        `}></div>
      )}
      <ul className="flex flex-col gap-y-1 p-4">
        {
          menuItems.map((menuItem) => <MenuItem
            key={menuItem.id}
            active={false}
            level={level}
            {...{menuItemClickHandler}}
            {...{menuItem}}

          />)
        }
      </ul>
    </div>
  );
}

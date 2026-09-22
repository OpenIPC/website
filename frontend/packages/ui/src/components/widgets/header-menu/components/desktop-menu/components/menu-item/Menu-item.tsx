import type { MenuItem } from '../../../../Header-menu';
import UIIcons from '../../../../../../../assets/icons/ui';
import { debounce } from '../../../../../../../utils';
import { useState, useRef } from 'preact/hooks';
import SubMenu from '../sub-menu';
import { TargetedMouseEvent } from 'preact';

interface MenuItemProps {
  menuItem: MenuItem;
  active: boolean;
  level?: number;
  menuItemClickHandler?: () => void;
}

export default function MenuItem(
  { menuItem, active, menuItemClickHandler, level = 1 }: MenuItemProps
) {
  const { label, type, url, children } = menuItem;
  const { Triangle, TriangleRight } = UIIcons;

  // fancyweb-ng dimmed the labels to opacity-60 at rest and restored them on
  // hover. White at 60% over --color-brand-blue is #b7bfef, which is 2.95:1
  // against that ground -- under the 3:1 large-text floor, never mind AA's
  // 4.5:1. Full white is 5.29:1, so the labels stay readable and the hover
  // affordance moves to the underline the active item already uses.
  const directionStyle: Record<'line'|'column', string> = {
    column: `flex flex-row items-center justify-between gap-x-1`,
    line: `flex w-max flex-row items-center gap-x-1`,
  };

  const [isSubMenuVisible, setIsSubMenuVisible] = useState(false);
  const [fn, timeoutObj] = debounce(setIsSubMenuVisible, 350);

  // The anchor navigates. fancyweb-ng called preact-iso's route() here and
  // swallowed the click; this package carries no SPA router, and a static site
  // wants the browser to follow the href.
  const handleMenuItemClick = (_e?: TargetedMouseEvent<HTMLAnchorElement>) => {
    setIsSubMenuVisible(false);
    if (menuItemClickHandler) menuItemClickHandler();
  }

  const handleMouseLeave = () => {
    if (isSubMenuVisible) {
      fn(false);
    } else {
      clearTimeout(timeoutObj.current)
      setIsSubMenuVisible(false);
    }
  };

  const liRef = useRef<HTMLLIElement>(null);

  return (
    <li class="relative"
      {...(children && {
        onMouseEnter: () => fn(true),
        onMouseLeave: handleMouseLeave,
      })}
      ref={liRef}
    >
      <div
        className={`
          ${
            level > 1
              ? directionStyle['column']
              : directionStyle['line']
          }
        `}
      >
        <div className="relative">
          {
            (type === 'link' || type === 'mixed')
              ? <a
                  href={url}
                  className="
                    w-max cursor-pointer tracking-wide text-white decoration-1
                    underline-offset-[6px]
                    hover:underline
                  "
                  onClick={handleMenuItemClick}
                >
                  {label}
                </a>
              : children
                // A parent entry opens a submenu, so it is a button. It was
                // a span, and the submenu opened on mouseenter alone, which
                // left every child link of About and Tools unreachable
                // without a pointer.
                ? <button
                    type="button"
                    className="w-max cursor-pointer tracking-wide text-white"
                    aria-expanded={isSubMenuVisible}
                    aria-haspopup="true"
                    onClick={() => setIsSubMenuVisible(!isSubMenuVisible)}
                    onKeyDown={(e: KeyboardEvent) => {
                      if (e.key === 'Escape') { setIsSubMenuVisible(false); return; }
                      if (e.key !== 'ArrowDown') return;
                      e.preventDefault();
                      setIsSubMenuVisible(true);
                    }}
                  >
                    {label}
                  </button>
                : <span
                    className="w-max cursor-default tracking-wide text-white"
                  >
                    {label}
                  </span>
          }
          {
            active
              && <span
                className="absolute bottom-0 left-0 h-px w-full bg-white">
              </span>
          }
        </div>
        { children && level === 1 && <Triangle />}
        { children && level === 2 && <TriangleRight />}
      </div>
      {(children && isSubMenuVisible) && (
          <SubMenu
            level={level+1}
            menuItems={children}
            parent={liRef}
            menuItemClickHandler={handleMenuItemClick}
          />
      )}
    </li>
  );
}

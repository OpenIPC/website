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
    <li class="group relative"
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
      {/*
        Always in the DOM, hidden with CSS rather than rendered on demand.

        It used to be `{children && isSubMenuVisible && <SubMenu/>}`, which
        meant the child links did not exist in the served HTML: openipc.org's
        navigation has 34 addresses and a prerendered page carried 6 of them.
        Every page behind a dropdown -- teleoperation, edge AI, the Open Wall,
        the whole services set, all three tools -- was linked from nothing a
        crawler could see, on a site whose reason for being prerendered at all
        is search discovery. Bootstrap's dropdown, which this replaced, renders
        the whole tree and hides it with CSS; so does this now.

        The visibility rules are doubled on purpose. `group-hover` and
        `group-focus-within` open it with no JavaScript at all, which is what
        makes the menu work on a page whose island has not hydrated yet or
        never will; the state class opens it for the click and the keyboard,
        which is what makes it work on a touch screen, where there is no hover.

        `invisible` rather than `hidden`: it keeps the subtree in the
        accessibility and find-in-page trees' reach while taking it out of the
        pointer's, and it is what allows the fade.
      */}
      {children && (
        <div
          className={`
            transition-opacity duration-150
            group-focus-within:visible group-focus-within:opacity-100
            group-hover:visible group-hover:opacity-100
            ${isSubMenuVisible ? 'visible opacity-100' : 'invisible opacity-0'}
          `}
        >
          <SubMenu
            level={level+1}
            menuItems={children}
            parent={liRef}
            menuItemClickHandler={handleMenuItemClick}
          />
        </div>
      )}
    </li>
  );
}

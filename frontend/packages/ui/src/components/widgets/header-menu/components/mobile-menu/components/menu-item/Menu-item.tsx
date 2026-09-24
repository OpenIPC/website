import type { MenuItem } from '../../../../Header-menu';
import UIIcons from '../../../../../../../assets/icons/ui';
import { useState } from 'preact/hooks';

interface MenuItemProps {
  menuItem: MenuItem;
  active: boolean;
  level?: number;
  toggleMenu?: () => void;
}

export default function MenuItem(
  { menuItem, active, toggleMenu, level = 0 }: MenuItemProps
) {
  const { label, type, url, children } = menuItem;
  const { Triangle } = UIIcons;

  const [ isExpanded, setIsExpanded ] = useState(false);
  const [ shouldRender, setShouldRender ] = useState(false);
  const [ isAnimating, setIsAnimating ] = useState(false);

  const toggleSubMenu = () => {
    if (!isExpanded) {
      setShouldRender(true);
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          setIsAnimating(true);
        });
      });
    } else {
      setIsAnimating(false);
      setTimeout(() => {
        setShouldRender(false);
      }, 300);
    }
    setIsExpanded(!isExpanded);
  }

  // Upstream called preventDefault() here and then only closed the drawer, so
  // every link in the mobile menu went nowhere. Let the anchor navigate.
  const handleMenuItemClick = () => {
    if (toggleMenu) toggleMenu();
  }

  return (
    <li className="">
      {/*
        No onClick here. It used to carry one, and so does the button inside
        it, so a tap on a parent ran toggleSubMenu twice -- the button's
        handler, then the same handler again as the event bubbled -- which
        opened the group and closed it in the same gesture. On a phone that
        made every group a dead end: /donate, /our-team, the five services
        pages and the three tools were unreachable from the menu.

        The button now holds the label and the triangle, so the whole row is
        one control with one handler.
      */}
      <div
        // Not dimmed: white at opacity-60 over --color-brand-blue is 2.95:1,
        // and these are the site's navigation labels. See the desktop item.
        className="flex w-max flex-row items-center gap-x-1"
      >
        {
          level > 0
          && new Array(level)
            .fill(0)
            // eslint-disable-next-line @eslint-react/no-array-index-key -- indentation spacers; position is the whole of their identity
            .map((_, i) => <div className="pl-3" key={`indent-${i}`}></div>)
        }
        <div className="relative">
          {
            (type === 'link' || type === 'mixed')
              ? <a
                  href={url}
                  className="w-max cursor-pointer tracking-wide text-white"
                  onClick={handleMenuItemClick}
                >
                  {label}
                </a>
              : children
                // A parent row expands its children, so it is a button. The
                // row's onClick did the expanding and a span cannot receive
                // it from a keyboard.
                ? <button
                    type="button"
                    className="
                      flex w-max cursor-pointer flex-row items-center gap-x-1
                      tracking-wide text-white
                    "
                    aria-expanded={isExpanded}
                    onClick={toggleSubMenu}
                  >
                    {label}
                    <Triangle />
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

      </div>
      {/*
        Rendered whenever the row has children, not only while it is open, for
        the same reason the desktop submenu is: the links have to be in the
        page rather than summoned by a gesture.

        max-h-240 (60rem) rather than max-h-40. The old cap was 10rem, which fits
        about five rows -- and Ecosystem carries six plus a nested group, so
        the bottom of the longest menu was clipped by the animation that was
        supposed to reveal it. The number only has to be larger than any group
        can be; the transition still reads as a slide.

        `invisible` as well as `max-h-0`, because overflow-hidden clips a thing
        without taking it out of the tab order: a closed group would otherwise
        be nine stops on the way past it for anyone using a keyboard, each of
        them landing on something they cannot see.
      */}
      {
        children
        && <div className={`
          overflow-hidden transition-all duration-300 ease-linear
          ${isAnimating && shouldRender
            ? 'visible max-h-240'
            : 'invisible max-h-0'
          }
        `}>
          <ul> {
            children.map(child => <MenuItem
              key={child.id}
              menuItem={child}
              active={false}
              level={level + 1}
              toggleMenu={toggleMenu}
            />)}
          </ul>
        </div>
      }
    </li>
  );
}

import { DesktopMenu } from './components';
import { MobileMenu } from './components';
import HeaderBurgerButton from '../header-burger-button';
import UIIcons from '../../../assets/icons/ui';
import { useEffect, useRef, useState } from 'preact/hooks';
import { useMediaQuery } from '../../../utils/hooks/useMediaQuery';

type ItemType = 'link' | 'parent' | 'mixed';

export interface MenuItem {
  id: string;
  label: string;
  type: ItemType;
  url?: string;
  children?: MenuItem[];
}

export type MenuItems = MenuItem[];

interface HeaderMenuProps {
  menuItems: MenuItems;
}

export default function HeaderMenu({ menuItems }: HeaderMenuProps) {
  const { Logo } = UIIcons;

  const [ isOpen, setIsOpen ] = useState(false);
  const [ shouldRender, setShouldRender ] = useState(false);
  const [ isAnimating, setIsAnimating ] = useState(false);
  const [ isBurgBtnOpened, setIsBurgBtnOpened ] = useState(false);
  const navRef = useRef(null);
  const unmountTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const isMobile = useMediaQuery('(width < 768px)');

  useEffect(() => () => clearTimeout(unmountTimerRef.current), []);

  const toggleMenu = () => {
    if (!isOpen) {
      clearTimeout(unmountTimerRef.current);
      setIsBurgBtnOpened(true);
      setShouldRender(true);
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          setIsAnimating(true);
        })
      });
    } else {
      setIsBurgBtnOpened(false);
      setIsAnimating(false);
      // Held so that reopening during the 500ms close can cancel it. Without
      // that, the stale callback fired afterwards and unmounted the drawer
      // the user had just reopened.
      unmountTimerRef.current = setTimeout(() => {
        setShouldRender(false);
      }, 500);
    }
    setIsOpen(!isOpen);
  }

  if (!isMobile) {
    setIsOpen(false);
    setShouldRender(false);
    setIsAnimating(false);
    // Left set, so returning to a mobile width showed an open burger with no
    // drawer behind it.
    setIsBurgBtnOpened(false);
  }

  return (
    <nav className="min-h-10 w-full bg-brand-blue" ref={navRef}>
      <div className="flex min-h-10 flex-row items-center justify-between">
        <div className="w-28 px-2" {...(shouldRender && {onClick: toggleMenu})}>
          <a href="/" className="block w-full">
            <Logo />
          </a>
        </div>
        { !isMobile && <DesktopMenu {...{menuItems}} /> }
        { isMobile && (
          <div className="
            mr-2 h-[22px] w-[29px]
            md:mr-0
          ">
            <HeaderBurgerButton
              clickHandler={toggleMenu}
              isOpen={isBurgBtnOpened}
            />
          </div>
        )}
      </div>
      { shouldRender && (
        <MobileMenu  {...{menuItems, isAnimating, toggleMenu, navRef}} />
      )}
    </nav>
  );
}

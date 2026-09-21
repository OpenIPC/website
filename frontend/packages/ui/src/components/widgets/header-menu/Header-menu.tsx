import { DesktopMenu } from './components';
import { MobileMenu } from './components';
import HeaderBurgerButton from '../header-burger-button';
import UIIcons from '../../../assets/icons/ui';
import { useState, useRef } from 'preact/hooks';
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
  const isMobile = useMediaQuery('(width < 768px)');

  const toggleMenu = () => {
    if (!isOpen) {
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
      setTimeout(() => {
        setShouldRender(false);
      }, 500);
    }
    setIsOpen(!isOpen);
  }

  if (!isMobile) {
    setIsOpen(false);
    setShouldRender(false);
    setIsAnimating(false);
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

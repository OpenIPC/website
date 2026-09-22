import type { MenuItems } from '../../Header-menu';
import MenuItem from './components/menu-item';

interface DesktopMenuProps {
  menuItems: MenuItems;
}

export default function DesktopMenu({
  menuItems
}: DesktopMenuProps) {
  return (
    <ul className={`flex flex-row items-center justify-end gap-x-3`}>
      {
        menuItems.map((menuItem) => <MenuItem
          key={menuItem.id}
          active={false}
          {...{menuItem}}
        />)
      }
    </ul>
  );
}

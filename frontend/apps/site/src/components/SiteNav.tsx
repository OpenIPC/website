import { Header, HeaderMenu, type MenuItems } from '@openipc/ui';

/**
 * The navigation bar, as one island (#160).
 *
 * Header and HeaderMenu are separate components in the package -- Header is
 * the ink-blue band, HeaderMenu is the logo, the links and the burger -- and
 * Astro hydrates a component, not a composition. Wrapping them here means one
 * `client:load` boundary instead of two, and the menu's open/closed state
 * stays inside it.
 *
 * It renders on the server first: useMediaQuery answers `false` before the
 * effect runs, so the prerendered HTML carries the desktop menu and a phone
 * swaps to the drawer on hydration. That is the package's documented trade and
 * the reason the menu must hydrate at all -- with JavaScript off, a visitor on
 * a phone gets the desktop menu, which is navigable rather than absent.
 */
export default function SiteNav({ menuItems }: { menuItems: MenuItems }) {
  return (
    <Header>
      <HeaderMenu menuItems={menuItems} />
    </Header>
  );
}

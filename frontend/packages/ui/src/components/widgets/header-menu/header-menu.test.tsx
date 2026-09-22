/**
 * The site navigation, in the two ways it was unusable upstream.
 *
 * Neither is a styling preference. The labels failed contrast outright, and
 * on a phone the links did not go anywhere at all.
 */
import { expect, test, describe } from 'vitest';
import { render, fireEvent } from '@testing-library/preact';
import DesktopMenuItem from './components/desktop-menu/components/menu-item';
import MobileMenuItem from './components/mobile-menu/components/menu-item';
import { MENU_ITEMS } from './constants';

const link = MENU_ITEMS.find(i => i.type === 'link' && i.url)!;

/**
 * White at opacity-60 over --color-brand-blue (#4c60d8) resolves to #b7bfef,
 * which is 2.95:1 against that ground -- below the 3:1 large-text floor and
 * well below AA's 4.5:1 for body text. Undimmed white is 5.29:1.
 */
describe('the labels are readable at rest', () => {
  test('the desktop item does not dim itself', () => {
    const { container } = render(<DesktopMenuItem menuItem={link} active={false} />);
    expect(container.innerHTML).not.toContain('opacity-60');
  });

  test('the mobile item does not dim itself', () => {
    const { container } = render(<MobileMenuItem menuItem={link} active={false} />);
    expect(container.innerHTML).not.toContain('opacity-60');
  });

  test('hover is signalled by an underline instead', () => {
    const { container } = render(<DesktopMenuItem menuItem={link} active={false} />);
    expect(container.innerHTML).toContain('hover:underline');
  });
});

describe('the links navigate', () => {
  test('the mobile item lets the anchor through', () => {
    // It called preventDefault() and then only closed the drawer, so every
    // link in the mobile menu was dead.
    let closed = false;
    const { container } = render(
      <MobileMenuItem menuItem={link} active={false} toggleMenu={() => { closed = true; }} />,
    );
    const anchor = container.querySelector('a') as HTMLAnchorElement;
    expect(anchor.getAttribute('href')).toBe(link.url);

    const click = new MouseEvent('click', { bubbles: true, cancelable: true });
    fireEvent(anchor, click);

    expect(click.defaultPrevented).toBe(false);
    expect(closed).toBe(true);
  });

  test('the desktop item lets the anchor through too', () => {
    const { container } = render(<DesktopMenuItem menuItem={link} active={false} />);
    const anchor = container.querySelector('a') as HTMLAnchorElement;
    const click = new MouseEvent('click', { bubbles: true, cancelable: true });
    fireEvent(anchor, click);

    expect(click.defaultPrevented).toBe(false);
  });
});

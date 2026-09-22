import type { Meta, StoryObj } from '@storybook/preact-vite';
import Header from './Header';
import HeaderMenu from '../header-menu';
import { MENU_ITEMS } from '../header-menu/constants';

const meta: Meta<typeof Header> = {
  component: Header,
  title: 'Design System/Widgets/Header',
  decorators: [
    (Story) => (
      <>
        <div class="bg-brand-blue">
          <Story/>
        </div>
        <div className="min-h-screen bg-crimson">
        </div>
      </>
    ),
  ],
}

export default meta;

type Story = StoryObj<typeof Header>;

export const HeaderStory: Story = {
  args: { children: <HeaderMenu menuItems={MENU_ITEMS} /> },
};

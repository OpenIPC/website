import { Meta, StoryObj } from '@storybook/preact-vite';
import HeaderMenu from './Header-menu';
import { MENU_ITEMS } from './constants'; 

const meta: Meta<typeof HeaderMenu> = {
  component: HeaderMenu,
  title: 'Design System/Widgets/Header Menu',
  decorators: [
    (Story) => (
      <div className="bg-brand-blue" id="app">
        <Story />
        <div>
          <h1>
            Lorem ipsum dolor
          </h1>
          <p>
sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum.
          </p>
          <p>
sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum.
          </p>
          <p>
sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum.
          </p>
        </div>
      </div>
    ),
  ],
}

export default meta;

type Story = StoryObj<typeof HeaderMenu>;

export const HeaderMenuStory: Story = {
  args: {
    menuItems: MENU_ITEMS,
  }
};

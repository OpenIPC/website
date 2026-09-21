import { Meta, StoryObj } from '@storybook/preact-vite';
import SubMenu from './Sub-menu';
import { MENU_ITEMS } from '../../../../constants';

const meta: Meta<typeof SubMenu> = {
  component: SubMenu,
  title: 'Design System/Widgets/Header Menu/Sub Menu ',
  decorators: [
    (Story) => (
      <div className="bg-brand-blue">
        <div>
          <h1>
            Lorem ipsum dolor
          </h1>
          <Story />
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

type Story = StoryObj<typeof  SubMenu>;

export const SubMenuStory: Story = {
  args: {
    level: 2,
    menuItems: MENU_ITEMS[3].children,
  },
};
